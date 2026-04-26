package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RunUpdatePipeline executes the 4-stage update pipeline:
//
//	Stage 1 (Compare)  → classifies entries, routes to hash or bypass channel
//	Stage 2 (Hash)     → worker pool computes file hashes
//	Stage 3 (Reorder)  → reassembles entries in path-sorted order
//	Stage 4 (Write)    → serialises entries and writes to temp index file
//
// On success, the temp file is atomically renamed to main.idx.
// On failure, the temp file is deleted.
func RunUpdatePipeline(ctx context.Context, dc *DirectoryCache, leftIter, rightIter BinaryEntryIterator, tempPath string) error {
	// Channel buffer size: absorbs burst between stages without excessive memory
	const bufSize = 100

	hashCh := make(chan *PipelineEntry, bufSize)
	bypassCh := make(chan *PipelineEntry, bufSize)
	hashedCh := make(chan *PipelineEntry, bufSize)
	retiredCh := make(chan *PipelineEntry, bufSize)

	// Derive a cancellable context so any stage failure stops all others
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var errs []error
	var errMu sync.Mutex
	var wg sync.WaitGroup

	recordErr := func(err error) {
		if err != nil {
			errMu.Lock()
			errs = append(errs, err)
			errMu.Unlock()
			cancel() // signal all stages to stop
		}
	}

	// --- Stage 1: Compare (runs in this goroutine via hwangLinUnified) ---
	// We run comparison in a goroutine so the other stages can start concurrently.
	wg.Add(1)
	go func() {
		defer wg.Done()
		sink := newScanWriteSink(nil, scanWriteCanonical, hashCh, bypassCh)
		adapter := newSinkCallbackAdapter(sink)
		err := hwangLinUnified(leftIter, rightIter, adapter, ctx)
		if err != nil {
			// If the error is from context cancellation, don't double-report
			if ctx.Err() == nil {
				recordErr(fmt.Errorf("comparison stage: %w", err))
			}
			// Ensure channels are closed even on error so downstream stages unblock
			// (OnComplete/Close may not have been called if hwangLinUnified returned early)
			// sink.Close() is idempotent via the adapter's OnComplete
		}
	}()

	// --- Stage 2: Hash Pool ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		pool := newHashPool(dc, hashCh, hashedCh, dc.hashWorkers)
		if err := pool.Run(ctx); err != nil && ctx.Err() == nil {
			recordErr(fmt.Errorf("hash stage: %w", err))
		}
	}()

	// --- Stage 3: Reorder Buffer ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		rb := newReorderBuffer(retiredCh)
		if err := rb.Run(ctx, bypassCh, hashedCh); err != nil && ctx.Err() == nil {
			recordErr(fmt.Errorf("reorder stage: %w", err))
		}
	}()

	// --- Stage 4: Write ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runWriteStage(ctx, dc, tempPath, retiredCh)
		if err != nil && ctx.Err() == nil {
			recordErr(fmt.Errorf("write stage: %w", err))
		}
	}()

	wg.Wait()

	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// runWriteStage reads ordered entries from retiredCh, serialises them,
// and writes them to a temp index file using TempIndexWriter.
func runWriteStage(ctx context.Context, dc *DirectoryCache, tempPath string, retiredCh <-chan *PipelineEntry) error {
	writer, err := NewTempIndexWriter(dc, tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temp index writer: %w", err)
	}

	serialiser := NewEntrySerialiser()

	// Batch entries before writing for efficiency
	const batchSize = 64
	batch := make([][]byte, 0, batchSize)

	flushBatch := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := writer.WriteSerialised(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			// Don't close the writer properly — the temp file will be cleaned up
			_ = writer.Close()
			return ctx.Err()
		case pe, ok := <-retiredCh:
			if !ok {
				// All entries received — flush remaining and finalise
				if err := flushBatch(); err != nil {
					_ = writer.Close()
					return err
				}
				return writer.Close()
			}

			data, err := serialiser.Serialise(pe.Entry)
			if err != nil {
				_ = writer.Close()
				return fmt.Errorf("failed to serialise entry: %w", err)
			}

			// Mark deleted entries in the serialised copy (safe — it's heap-allocated)
			if pe.Operation == OpDeleted {
				markSerialisedDeleted(data)
			}

			pe.WriteData = data
			batch = append(batch, data)

			if len(batch) >= batchSize {
				if err := flushBatch(); err != nil {
					_ = writer.Close()
					return err
				}
			}
		}
	}
}

// finaliseMainIndex handles the success/failure branches of the
// main-index cache lifecycle: on success, atomically rename the
// timestamped temp file into place and clean up related temp files;
// on failure, remove the incomplete temp file. logPrefix tags stderr
// diagnostics so pipeline vs legacy-update call sites stay
// distinguishable in captured logs.
func finaliseMainIndex(dc *DirectoryCache, tempName, logPrefix string, ok bool) {
	if !ok {
		if _, err := os.Stat(tempName); err != nil {
			return
		}
		if removeErr := os.Remove(tempName); removeErr != nil && IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "%s Warning: failed to remove incomplete main index %s: %v\n", logPrefix, tempName, removeErr)
		} else if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "%s Removed incomplete main index: %s\n", logPrefix, filepath.Base(tempName))
		}
		return
	}
	stat, err := os.Stat(tempName)
	if err != nil {
		return
	}
	if IsDebugEnabled("write") {
		VerboseLog(3, "%s Renaming %s (%d bytes) -> %s", logPrefix, tempName, stat.Size(), dc.IndexFile)
	}
	if renameErr := os.Rename(tempName, dc.IndexFile); renameErr != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "%s Warning: failed to rename %s to main.idx: %v\n", logPrefix, tempName, renameErr)
		}
		return
	}
	if cleanupErr := dc.CleanupTimestampedCacheFiles(); cleanupErr != nil && IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "%s Warning: failed to cleanup timestamped cache files: %v\n", logPrefix, cleanupErr)
	}
}

// performPipelineScan replaces performUnifiedScanToSkiplist with the pipeline architecture.
func (dc *DirectoryCache) performPipelineScan(ctx context.Context, paths []string, compareSkiplist *skiplistWrapper) error {
	defer VerboseEnter()()

	// Synchronise concurrent scans
	dc.scanMutex.Lock()
	defer dc.scanMutex.Unlock()

	if dc.scanInProgress {
		if dc.lastScanError != nil {
			return dc.lastScanError
		}
		return nil
	}

	dc.scanInProgress = true
	defer func() { dc.scanInProgress = false }()

	// Generate timestamped main index filename
	tempMainIndexFileName := dc.GenerateTimestampedFileName("main")

	var operationSuccessful bool
	defer func() { finaliseMainIndex(dc, tempMainIndexFileName, "[PIPELINE]", operationSuccessful) }()

	// Create iterators
	existingIterator := NewBinaryEntrySkiplistIterator(ctx, compareSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(ctx, dc, paths, "scan")

	// Run the pipeline
	err := RunUpdatePipeline(ctx, dc, existingIterator, scanIterator, tempMainIndexFileName)
	if err != nil {
		dc.lastScanError = err
		return err
	}

	operationSuccessful = true
	dc.lastScanError = nil
	return nil
}
