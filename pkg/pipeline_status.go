package dircachefilehash

import (
	"context"
	"fmt"
	"sync"
)

// RunStatusPipeline executes the 4-stage status pipeline:
//
//	Stage 1 (Compare)  → main.idx vs filesystem, with cache as hash lookup
//	Stage 2 (Hash)     → worker pool computes file hashes for uncached changes
//	Stage 3 (Reorder)  → reassembles entries in path-sorted order
//	Stage 4 (Write)    → serialises entries and writes to temp cache index file
//
// Only entries that differ from main are written to the cache (sparse delta).
// The resulting cache file IS the status — its entries are the changes.
// The caller derives StatusResult by reading the cache after the pipeline completes.
func RunStatusPipeline(ctx context.Context, ms *MetaStore, sr *ScanRun, cacheSkiplist *skiplistWrapper, leftIter, rightIter BinaryEntryIterator, tempPath string) error {
	const bufSize = 100

	hashCh := make(chan *PipelineEntry, bufSize)
	bypassCh := make(chan *PipelineEntry, bufSize)
	hashedCh := make(chan *PipelineEntry, bufSize)
	retiredCh := make(chan *PipelineEntry, bufSize)

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
			cancel()
		}
	}

	sink := newScanWriteSink(cacheSkiplist, scanWriteDelta, hashCh, bypassCh)

	// --- Stage 1: Compare (main.idx vs filesystem) ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		adapter := newSinkCallbackAdapter(sink)
		err := hwangLin(leftIter, rightIter, adapter, ctx)
		if err != nil && ctx.Err() == nil {
			recordErr(fmt.Errorf("comparison stage: %w", err))
		}
	}()

	// --- Stage 2: Hash Pool ---
	wg.Add(1)
	go func() {
		defer wg.Done()
		pool := newHashPool(ms, sr, hashCh, hashedCh, sr.HashWorkers)
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
		err := runWriteStage(ctx, ms, tempPath, retiredCh)
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
