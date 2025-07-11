package dircachefilehash

import (
	"fmt"
	"os"
	"sync"
)

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
	completedQueue      []uint64               // Jobs completed but waiting for order
	nextExpectedJobID   uint64                 // Next JobID expected in sequence
	iteratorNotifyChans []chan<- uint64        // Channels to signal iterators
	queueMutex          sync.Mutex             // Protect completed queue and iterator channels
	
	// Internal coordination
	completionChan chan uint64            // Internal channel for processing completions
	processorWg    sync.WaitGroup         // Wait group for completion processor
}

// newAlgorithmHashManager creates a new algorithm-specific hash manager
func (dc *DirectoryCache) newAlgorithmHashManager(numWorkers int, shutdownChan <-chan struct{}) *algorithmHashManager {
	manager := &algorithmHashManager{
		hashJobChan:         make(chan *hashJobStart, 100),
		callFinishChan:      make(chan uint64, 100),
		shutdownChan:        shutdownChan,
		completedQueue:      make([]uint64, 0),
		nextExpectedJobID:   1, // JobIDs start at 1
		iteratorNotifyChans: make([]chan<- uint64, 0),
		completionChan: make(chan uint64, 100),
	}
	
	// Start hash workers (same as simpleHashManager)
	for i := 0; i < numWorkers; i++ {
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
	
	for {
		select {
		case jobID, ok := <-ahm.completionChan:
			if !ok {
				// Channel closed, processor shutting down
				return
			}
			
			ahm.processCompletion(jobID)
			
		case <-ahm.shutdownChan:
			// Shutdown requested
			return
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

// signalIterators sends a completion notification to all registered iterators
func (ahm *algorithmHashManager) signalIterators(jobID uint64) {
	if IsDebugEnabled("algorithm") {
		fmt.Fprintf(os.Stderr, "[ALGORITHM] Signaling JobID %d to %d iterators\n", jobID, len(ahm.iteratorNotifyChans))
	}
	
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

// SubmitHashJob submits a hash job for processing
func (ahm *algorithmHashManager) SubmitHashJob(job *hashJobStart) {
	ahm.hashJobChan <- job
	// Note: We don't send to callStartChan here as that's handled by the processor
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

			// Check if this is a symlink by examining the file mode
			if job.ScannedPath.Info.Mode()&os.ModeSymlink != 0 {
				// This is a symlink - hash the target path
				hashBytes, hashType, err = dc.hashSymlinkTargetToBytes(job.FilePath)
			} else {
				// Regular file - hash the file contents with interruptible hashing
				hashBytes, hashType, err = dc.HashFileInterruptibleToBytes(job.FilePath, ahm.shutdownChan)
			}

			if err == nil {
				// Update the binaryEntry directly in the scan index mmap memory
				// This provides zero-copy updates to the scan index file
				if updateErr := dc.updateBinaryEntryHash(job.IndexEntry, hashBytes, hashType); updateErr != nil {
					fmt.Fprintf(os.Stderr, "[ERROR] Failed to update binary entry hash: %v\n", updateErr)
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
	
	// Wait for hash workers to finish
	ahm.wg.Wait()
	
	// Close completion processor
	close(ahm.completionChan)
	ahm.processorWg.Wait()
	
	if IsDebugEnabled("algorithm") {
		fmt.Fprintf(os.Stderr, "[ALGORITHM] Hash manager shutdown complete\n")
	}
}

// Wait waits for all hash workers to complete
func (ahm *algorithmHashManager) Wait() {
	ahm.wg.Wait()
	ahm.processorWg.Wait()
}

// GetQueueStats returns statistics about the completed queue for debugging
func (ahm *algorithmHashManager) GetQueueStats() (queueSize int, nextExpected uint64, registeredIterators int) {
	ahm.queueMutex.Lock()
	defer ahm.queueMutex.Unlock()
	
	return len(ahm.completedQueue), ahm.nextExpectedJobID, len(ahm.iteratorNotifyChans)
}