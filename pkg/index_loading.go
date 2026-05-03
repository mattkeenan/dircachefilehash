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

// loadIndexShared returns the cached *Index for path, loading and
// memoising on first call or stat mismatch. The memo owns the mapping;
// callers must NOT DecRef the returned Index's File. Skiplist entries
// built from idx.Refs remain valid as long as the MetaStore is open:
// stat-mismatch evictions move the old entry to orphanIndices rather
// than unmapping immediately, and Close drains both maps.
func (ms *MetaStore) loadIndexShared(path string) (*Index, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	stat, _ := statForMemo(info)

	ms.loadedMu.Lock()
	defer ms.loadedMu.Unlock()

	if ms.loadedIndices == nil {
		ms.loadedIndices = make(map[string]*Index)
	}

	if cached, ok := ms.loadedIndices[path]; ok {
		if cached.Stat == stat {
			return cached, nil
		}
		ms.orphanIndices = append(ms.orphanIndices, cached)
		delete(ms.loadedIndices, path)
	}

	idx, err := ms.loadIndexFromFileWithTracking(path)
	if err != nil {
		return nil, err
	}
	idx.Stat = stat
	ms.loadedIndices[path] = idx
	return idx, nil
}

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (ms *MetaStore) LoadMainIndex() (*skiplistWrapper, error) {
	idx, err := ms.loadIndexShared(ms.IndexFile)
	if os.IsNotExist(err) {
		if err := ms.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
		idx, err = ms.loadIndexShared(ms.IndexFile)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// ms.mainIndex is a non-owning per-type pointer used by IndexTimestamp
	// (pkg/metastore.go) for the mmap RWMutex. The memo owns lifetime.
	if idx.File != nil {
		idx.File.Type = "main"
		ms.registerIndex("main", idx.File)
	}

	return buildSkiplistFromRefs(idx.Refs, MainContext), nil
}

// LoadMergedMainCacheIndex loads the main index and merges the cache index on
// top, giving callers the complete existing file state without scanning.
func (ms *MetaStore) LoadMergedMainCacheIndex() (*skiplistWrapper, error) {
	// Load main index as base
	mergedSkiplist, err := ms.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Load cache index and merge into the merged skiplist (avoid .Copy() - merge directly)
	cacheSkiplist, err := ms.loadCacheIndex()
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
func (ms *MetaStore) loadCacheIndex() (*skiplistWrapper, error) {
	skiplist := NewSkiplistWrapper(16, CacheContext)

	idx, err := ms.loadIndexShared(ms.CacheFile)
	switch {
	case err == nil:
		if idx.File != nil {
			idx.File.Type = "cache"
			ms.registerIndex("cache", idx.File)
		}
		for _, ref := range idx.Refs {
			skiplist.Insert(ref, CacheContext)
		}
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: loaded %d entries from cache.idx", len(idx.Refs))
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Load and merge timestamped cache files in chronological order
	timestampedCaches, err := ms.ScanForTimestampedCacheFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan for timestamped cache files: %w", err)
	}

	for _, cacheFile := range timestampedCaches {
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merging timestamped cache file: %s", filepath.Base(cacheFile))
		}

		idx, err := ms.loadIndexShared(cacheFile)
		if err != nil {
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[CACHE] Warning: skipping corrupted cache file %s: %v\n", cacheFile, err)
			}
			continue
		}

		if idx.File != nil {
			idx.File.Type = "timestamped-cache"
			ms.registerIndex(fmt.Sprintf("timestamped-cache-%s", filepath.Base(cacheFile)), idx.File)
		}

		timestampedSkiplist := buildSkiplistFromRefs(idx.Refs, CacheContext)

		if err := skiplist.Merge(timestampedSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge timestamped cache file %s: %w", cacheFile, err)
		}

		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merged %d entries from %s", len(idx.Refs), filepath.Base(cacheFile))
		}
	}

	if IsDebugEnabled("load") && len(timestampedCaches) > 0 {
		VerboseLog(3, "loadCacheIndex: final merged cache has %d entries", skiplist.Length())
	}

	return skiplist, nil
}
