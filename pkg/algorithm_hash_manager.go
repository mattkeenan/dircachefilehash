package dircachefilehash

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// hashJobCompletion represents a completed hash job with both system JobID and caller Cookie
type hashJobCompletion struct {
	JobID  uint64 // System job ID
	Cookie uint64 // Caller's cookie (echoed back)
}

// algorithmHashManager extends simpleHashManager with ordered completion notifications
// for streaming iterators. It maintains a completed queue to ensure notifications
// are sent to iterators in JobID order, even though hash jobs complete out of order.
type algorithmHashManager struct {
	// Base hash manager functionality (copied from simpleHashManager)
	hashJobChan    chan *hashJobStart
	callFinishChan chan uint64 // job completion notifications (internal)
	wg             sync.WaitGroup
	shutdownChan   <-chan struct{} // shutdown notification
	closed         bool            // track if channel is closed
	closeMutex     sync.Mutex      // protect closed flag

	// New fields for ordered completion notifications
	completedQueue      []uint64        // Jobs completed but waiting for order
	nextExpectedJobID   uint64          // Next JobID expected in sequence
	iteratorNotifyChans []chan<- uint64 // Channels to signal iterators
	queueMutex          sync.Mutex      // Protect completed queue and iterator channels

	// Cookie tracking for external callers
	jobIDToCookie          map[uint64]uint64      // Maps JobID to caller's Cookie
	externalCompletionChan chan hashJobCompletion // External completion notifications with cookies

	// Internal coordination
	completionChan chan uint64    // Internal channel for processing completions
	processorWg    sync.WaitGroup // Wait group for completion processor

	// Job ID allocation
	nextJobID uint64 // Monotonically increasing job ID counter (use atomic operations)
}

// newAlgorithmHashManager creates a new algorithm-specific hash manager
func (dc *DirectoryCache) newAlgorithmHashManager(numWorkers int, shutdownChan <-chan struct{}) *algorithmHashManager {
	manager := &algorithmHashManager{
		hashJobChan:            make(chan *hashJobStart, 100),
		callFinishChan:         make(chan uint64, 100),
		shutdownChan:           shutdownChan,
		completedQueue:         make([]uint64, 0),
		nextExpectedJobID:      1, // JobIDs start at 1
		iteratorNotifyChans:    make([]chan<- uint64, 0),
		jobIDToCookie:          make(map[uint64]uint64),
		externalCompletionChan: make(chan hashJobCompletion, 100),
		completionChan:         make(chan uint64, 100),
		nextJobID:              0, // Start job ID allocation at 0 so first GetNextJobID() returns 1
	}

	// Start hash workers (same as simpleHashManager)
	for range numWorkers {
		manager.wg.Add(1)
		go manager.hashWorker(dc)
	}

	// Start completion processor
	manager.processorWg.Add(1)
	go manager.completionProcessor()

	return manager
}

// RegisterIteratorNotification registers a channel to receive ordered completion notifications
func (ahm *algorithmHashManager) RegisterIteratorNotification(notifyChan chan<- uint64) {
	ahm.queueMutex.Lock()
	defer ahm.queueMutex.Unlock()

	ahm.iteratorNotifyChans = append(ahm.iteratorNotifyChans, notifyChan)
}

// UnregisterIteratorNotification removes a channel from receiving notifications
func (ahm *algorithmHashManager) UnregisterIteratorNotification(notifyChan chan<- uint64) {
	ahm.queueMutex.Lock()
	defer ahm.queueMutex.Unlock()

	for i, ch := range ahm.iteratorNotifyChans {
		if ch == notifyChan {
			// Remove from slice
			ahm.iteratorNotifyChans = append(ahm.iteratorNotifyChans[:i], ahm.iteratorNotifyChans[i+1:]...)
			break
		}
	}
}

// completionProcessor processes hash job completions and sends ordered notifications
func (ahm *algorithmHashManager) completionProcessor() {
	defer ahm.processorWg.Done()
	defer close(ahm.externalCompletionChan) // Close external completion channel on exit

	for {
		select {
		case jobID, ok := <-ahm.completionChan:
			if !ok {
				// Channel closed, processor shutting down
				return
			}

			ahm.processCompletion(jobID)

		case <-ahm.shutdownChan:
			// Shutdown requested - drain any remaining completions quickly
			for {
				select {
				case jobID, ok := <-ahm.completionChan:
					if !ok {
						return
					}
					ahm.processCompletion(jobID)
				default:
					// No more completions to process
					return
				}
			}
		}
	}
}

// processCompletion handles a single completion and potentially flushes the queue
func (ahm *algorithmHashManager) processCompletion(jobID uint64) {
	ahm.queueMutex.Lock()
	defer ahm.queueMutex.Unlock()

	if IsDebugEnabled("algorithm") {
		fmt.Fprintf(os.Stderr, "[ALGORITHM] Processing completion for JobID %d, expected %d\n", jobID, ahm.nextExpectedJobID)
	}

	if jobID == ahm.nextExpectedJobID {
		// This is the next expected job - signal it immediately
		ahm.signalIterators(jobID)
		ahm.nextExpectedJobID++

		// Now flush any consecutive jobs from the completed queue
		ahm.flushCompletedQueue()
	} else {
		// This job completed out of order - add to completed queue
		ahm.addToCompletedQueue(jobID)

		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] JobID %d queued (out of order), queue size: %d\n", jobID, len(ahm.completedQueue))
		}
	}
}

// addToCompletedQueue adds a JobID to the completed queue in sorted order
func (ahm *algorithmHashManager) addToCompletedQueue(jobID uint64) {
	// Insert in sorted order (binary search would be more efficient for large queues)
	insertPos := len(ahm.completedQueue)
	for i, queuedJobID := range ahm.completedQueue {
		if jobID < queuedJobID {
			insertPos = i
			break
		}
	}

	// Insert at the correct position
	ahm.completedQueue = append(ahm.completedQueue, 0)
	copy(ahm.completedQueue[insertPos+1:], ahm.completedQueue[insertPos:])
	ahm.completedQueue[insertPos] = jobID
}

// flushCompletedQueue processes consecutive jobs from the completed queue
func (ahm *algorithmHashManager) flushCompletedQueue() {
	for len(ahm.completedQueue) > 0 && ahm.completedQueue[0] == ahm.nextExpectedJobID {
		// Found the next expected job in the queue
		jobID := ahm.completedQueue[0]
		ahm.completedQueue = ahm.completedQueue[1:] // Remove from front

		ahm.signalIterators(jobID)
		ahm.nextExpectedJobID++

		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] Flushed JobID %d from queue, queue size: %d\n", jobID, len(ahm.completedQueue))
		}
	}
}

// signalIterators sends a completion notification to all registered iterators and external completion channel
func (ahm *algorithmHashManager) signalIterators(jobID uint64) {
	if IsDebugEnabled("algorithm") {
		fmt.Fprintf(os.Stderr, "[ALGORITHM] Signaling JobID %d to %d iterators\n", jobID, len(ahm.iteratorNotifyChans))
	}

	// Send to iterator notification channels (existing behavior)
	for _, ch := range ahm.iteratorNotifyChans {
		select {
		case ch <- jobID:
			// Successfully sent notification
		default:
			// Channel full or closed - skip this iterator
			if IsDebugEnabled("algorithm") {
				fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Failed to notify iterator (channel full/closed)\n")
			}
		}
	}

	// Send to external completion channel with both JobID and Cookie
	cookie, hasCookie := ahm.jobIDToCookie[jobID]
	if hasCookie {
		completion := hashJobCompletion{
			JobID:  jobID,
			Cookie: cookie,
		}

		select {
		case ahm.externalCompletionChan <- completion:
			// Successfully sent external completion notification
			if IsDebugEnabled("algorithm") {
				fmt.Fprintf(os.Stderr, "[ALGORITHM] Sent external completion: JobID %d, Cookie %d\n", jobID, cookie)
			}
		default:
			// Channel full - skip this notification
			if IsDebugEnabled("algorithm") {
				fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Failed to send external completion (channel full)\n")
			}
		}

		// Remove the mapping since the job is completed
		delete(ahm.jobIDToCookie, jobID)
	}
}

// IsShuttingDown checks if the hash manager is shutting down
func (ahm *algorithmHashManager) IsShuttingDown() bool {
	select {
	case <-ahm.shutdownChan:
		return true
	default:
		return false
	}
}

// SubmitHashJob submits a hash job to the worker pool
// Note: The Cookie field in hashJobStart is typically the Hwang-Lin process path order ID,
// representing the sequential position after ignores, symlink updates, deletes, etc.
// The specific order depends on callback type requirements (Status, Update, Dupes).
func (ahm *algorithmHashManager) SubmitHashJob(job *hashJobStart) {
	// b) Actual hash job submission to manager
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-MANAGER] SubmitHashJob called: JobID=%d, Cookie=%d, FilePath=%s", job.JobID, job.Cookie, job.FilePath)
	}

	// Track the mapping from JobID to Cookie for completion notifications
	if job.Cookie != 0 {
		ahm.queueMutex.Lock()
		ahm.jobIDToCookie[job.JobID] = job.Cookie
		ahm.queueMutex.Unlock()

		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-MANAGER] Mapped JobID %d to Cookie %d", job.JobID, job.Cookie)
		}
	}

	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-MANAGER] Sending job to hash workers via hashJobChan")
	}

	ahm.hashJobChan <- job

	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-MANAGER] Hash job submitted successfully to workers")
	}
	// Note: We don't send to callStartChan here as that's handled by the processor
}

// GetNextJobID returns a unique job ID for hash job submission
func (ahm *algorithmHashManager) GetNextJobID() uint64 {
	return atomic.AddUint64(&ahm.nextJobID, 1)
}

// CompletionChannel returns a channel that provides completion notifications with both JobID and Cookie
func (ahm *algorithmHashManager) CompletionChannel() <-chan hashJobCompletion {
	return ahm.externalCompletionChan
}

// FinishSubmitting signals that no more hash jobs will be submitted
func (ahm *algorithmHashManager) FinishSubmitting() {
	ahm.closeMutex.Lock()
	defer ahm.closeMutex.Unlock()

	if !ahm.closed {
		close(ahm.hashJobChan)
		ahm.closed = true
	}
}

// hashWorker processes hash jobs (same as simpleHashManager)
func (ahm *algorithmHashManager) hashWorker(dc *DirectoryCache) {
	defer ahm.wg.Done()

	// Pre-allocate hash buffer for reuse across all files this worker processes
	bufferSize, err := dc.getHashBufferSize()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Failed to get hash buffer size: %v\n", err)
		return
	}
	buffer := make([]byte, bufferSize)

	var currentJob *hashJobStart // Track current job for interruption handling

	for {
		select {
		case job, ok := <-ahm.hashJobChan:
			if !ok {
				// Channel closed - no more jobs
				if IsDebugEnabled("algorithm") {
					fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash worker exiting - no more jobs\n")
				}
				return
			}

			currentJob = job

			if IsDebugEnabled("algorithm") {
				fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash started for file: %s (job %d)\n", job.FilePath, job.JobID)
			}

			// Hash the file and update binaryEntry directly in mmap memory
			// For symlinks, we hash the target path, not the target file contents
			var hashBytes []byte
			var hashType uint16
			var err error

			// Check if this is a symlink by examining the file mode from the entry
			var mode uint32
			if job.Entry != nil {
				mode, err = job.Entry.Mode()
				if err != nil {
					fmt.Fprintf(os.Stderr, "[ERROR] Failed to get file mode for hash job: %v\n", err)
					goto hashComplete
				}
			} else if job.ScannedPath != nil {
				// Fallback to ScannedPath for v0.6 compatibility
				mode = uint32(job.ScannedPath.Info.Mode())
			} else {
				fmt.Fprintf(os.Stderr, "[ERROR] Hash job has neither Entry nor ScannedPath\n")
				err = fmt.Errorf("invalid hash job: no entry or scanned path")
				goto hashComplete
			}

			if os.FileMode(mode)&os.ModeSymlink != 0 {
				// This is a symlink - hash the target path
				hashBytes, hashType, err = dc.hashSymlinkTargetToBytes(job.FilePath)
			} else {
				// Regular file - hash the file contents with interruptible hashing
				hashBytes, hashType, err = dc.HashFileInterruptibleToBytes(job.FilePath, ahm.shutdownChan, buffer)
			}

		hashComplete:

			if err == nil {
				// Update the entry with computed hash using unified BinaryEntryInterface
				if updateErr := job.Entry.SetHash(hashBytes, hashType); updateErr != nil {
					fmt.Fprintf(os.Stderr, "[ERROR] Failed to update entry hash: %v\n", updateErr)
				}
			}

			if err != nil {
				if IsDebugEnabled("algorithm") {
					fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash failed for file: %s (job %d): %v\n", job.FilePath, job.JobID, err)
				}
			} else {
				if IsDebugEnabled("algorithm") {
					fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash completed for file: %s (job %d)\n", job.FilePath, job.JobID)
				}
			}

			// Send completion notification to processor
			ahm.completionChan <- job.JobID
			currentJob = nil

		case <-ahm.shutdownChan:
			// Shutdown requested
			if currentJob != nil {
				if IsDebugEnabled("algorithm") {
					fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash worker interrupted, signaling completion for job %d\n", currentJob.JobID)
				}
				// Signal completion even if interrupted
				ahm.completionChan <- currentJob.JobID
			}
			return
		}
	}
}

// Shutdown gracefully shuts down the hash manager
func (ahm *algorithmHashManager) Shutdown() {
	ahm.closeMutex.Lock()
	if !ahm.closed {
		close(ahm.hashJobChan)
		ahm.closed = true
	}
	ahm.closeMutex.Unlock()

	// Wait for hash workers to finish with timeout
	done := make(chan struct{})
	go func() {
		ahm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Workers completed normally
	case <-time.After(60 * time.Second):
		// Timeout - continue anyway to prevent indefinite blocking
		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Hash worker shutdown timeout\n")
		}
	}

	// Close completion processor
	close(ahm.completionChan)

	// Wait for completion processor with timeout
	processorDone := make(chan struct{})
	go func() {
		ahm.processorWg.Wait()
		close(processorDone)
	}()

	select {
	case <-processorDone:
		// Completion processor finished normally
	case <-time.After(5 * time.Second):
		// Timeout - continue anyway
		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Completion processor shutdown timeout\n")
		}
	}

	// NOTE: externalCompletionChan is closed by completionProcessor via defer (line 100).
	// Do NOT close it here — that would cause a double-close panic.

	if IsDebugEnabled("algorithm") {
		fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash manager shutdown complete\n")
	}
}

// Wait waits for all hash workers to complete with timeout
func (ahm *algorithmHashManager) Wait() {
	// Wait for hash workers with timeout
	done := make(chan struct{})
	go func() {
		ahm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Workers completed normally
	case <-time.After(60 * time.Second):
		// Timeout - continue anyway
		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Hash worker wait timeout\n")
		}
	}

	// Wait for completion processor with timeout
	processorDone := make(chan struct{})
	go func() {
		ahm.processorWg.Wait()
		close(processorDone)
	}()

	select {
	case <-processorDone:
		// Completion processor finished normally
	case <-time.After(5 * time.Second):
		// Timeout - continue anyway
		if IsDebugEnabled("algorithm") {
			fmt.Fprintf(os.Stderr, "[ALGORITHM] Warning: Completion processor wait timeout\n")
		}
	}
}

// GetQueueStats returns statistics about the completed queue for debugging
func (ahm *algorithmHashManager) GetQueueStats() (queueSize int, nextExpected uint64, registeredIterators int) {
	ahm.queueMutex.Lock()
	defer ahm.queueMutex.Unlock()

	return len(ahm.completedQueue), ahm.nextExpectedJobID, len(ahm.iteratorNotifyChans)
}
