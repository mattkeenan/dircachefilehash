package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// statForMemo extracts identity for the read-only mmap memo from an
// os.FileInfo. Returns false if the underlying syscall.Stat_t is
// unavailable (non-Unix platforms — dcfh is Unix-only by design, so
// this should never trigger; defensive).
func statForMemo(info os.FileInfo) (cachedStat, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return cachedStat{}, false
	}
	return cachedStat{
		dev:   st.Dev,
		ino:   st.Ino,
		size:  info.Size(),
		mtime: info.ModTime().UnixNano(),
	}, true
}

// loadIndexShared returns the cached *mmapIndexFile + refs slice for path,
// loading and memoising on first call or stat mismatch. The memo owns the
// mapping; callers must NOT DecRef the returned file. Skiplist entries
// built from refs remain valid as long as the DirectoryCache is open:
// stat-mismatch evictions move the old entry to orphanIndices rather than
// unmapping immediately, and Close drains both maps.
func (dc *DirectoryCache) loadIndexShared(path string) (*mmapIndexFile, []binaryEntryRef, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	stat, _ := statForMemo(info)

	dc.loadedMu.Lock()
	defer dc.loadedMu.Unlock()

	if dc.loadedIndices == nil {
		dc.loadedIndices = make(map[string]*loadedIndex)
	}

	if cached, ok := dc.loadedIndices[path]; ok {
		if cached.stat == stat {
			return cached.file, cached.refs, nil
		}
		dc.orphanIndices = append(dc.orphanIndices, cached)
		delete(dc.loadedIndices, path)
	}

	refs, indexFile, err := dc.loadIndexFromFileWithTracking(path)
	if err != nil {
		return nil, nil, err
	}
	dc.loadedIndices[path] = &loadedIndex{
		file: indexFile,
		refs: refs,
		stat: stat,
	}
	return indexFile, refs, nil
}

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*skiplistWrapper, error) {
	indexFile, refs, err := dc.loadIndexShared(dc.IndexFile)
	if os.IsNotExist(err) {
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
		indexFile, refs, err = dc.loadIndexShared(dc.IndexFile)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// dc.mainIndex is a non-owning per-type pointer used by IndexTimestamp
	// (pkg/dircache.go) for the mmap RWMutex. The memo owns lifetime.
	if indexFile != nil {
		indexFile.Type = "main"
		dc.registerIndex("main", indexFile)
	}

	return buildSkiplistFromRefs(refs, MainContext), nil
}

// LoadMergedMainCacheIndex loads main index and merges cache index for unified architecture operations
// This provides a reusable pattern for operations that need complete existing file state without scanning
func (dc *DirectoryCache) LoadMergedMainCacheIndex() (*skiplistWrapper, error) {
	// Load main index as base
	mergedSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Load cache index and merge into the merged skiplist (avoid .Copy() - merge directly)
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		// Cache index might not exist, continue with just main index
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load cache index: %w", err)
		}
	} else {
		// Merge cache into the merged skiplist (name reflects its actual purpose)
		if err := mergedSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge cache index: %w", err)
		}
	}

	return mergedSkiplist, nil
}

// LoadCacheIndex loads the cache index file and merges timestamped cache files
func (dc *DirectoryCache) loadCacheIndex() (*skiplistWrapper, error) {
	skiplist := NewSkiplistWrapper(16, CacheContext)

	indexFile, refs, err := dc.loadIndexShared(dc.CacheFile)
	switch {
	case err == nil:
		if indexFile != nil {
			indexFile.Type = "cache"
			dc.registerIndex("cache", indexFile)
		}
		for _, ref := range refs {
			skiplist.Insert(ref, CacheContext)
		}
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: loaded %d entries from cache.idx", len(refs))
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Load and merge timestamped cache files in chronological order
	timestampedCaches, err := dc.ScanForTimestampedCacheFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan for timestamped cache files: %w", err)
	}

	for _, cacheFile := range timestampedCaches {
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merging timestamped cache file: %s", filepath.Base(cacheFile))
		}

		indexFile, refs, err := dc.loadIndexShared(cacheFile)
		if err != nil {
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[CACHE] Warning: skipping corrupted cache file %s: %v\n", cacheFile, err)
			}
			continue
		}

		if indexFile != nil {
			indexFile.Type = "timestamped-cache"
			dc.registerIndex(fmt.Sprintf("timestamped-cache-%s", filepath.Base(cacheFile)), indexFile)
		}

		timestampedSkiplist := buildSkiplistFromRefs(refs, CacheContext)

		if err := skiplist.Merge(timestampedSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge timestamped cache file %s: %w", cacheFile, err)
		}

		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merged %d entries from %s", len(refs), filepath.Base(cacheFile))
		}
	}

	if IsDebugEnabled("load") && len(timestampedCaches) > 0 {
		VerboseLog(3, "loadCacheIndex: final merged cache has %d entries", skiplist.Length())
	}

	return skiplist, nil
}
