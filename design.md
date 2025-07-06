# dircachefilehash design decisions

The dircachefilehash package (and hence the dcfh cli program), are designed to be able to track changes to files at massive scale (10s of millions of files, if not more). So there are a number of considerations to be made, firstly performance, flexibility, and security.

## background context

This package and cli program leverages a number of thought processes and methodologies used by git; namely, index files (i.e. a dircache) that reference the actual files, keeping the index entries ordered in bytewise memcmp() order (in dcfh's case by normalising filenames to UTF-8 using unix filepath separators, currently this is assumed as work on this will come in later versions of this package), and generally avoiding disk IO wherever feasible. And like git it uses the same "shortcut" of not rehashing files that have the same name (path), size, mtime/ctime, uid & gid (inodes / device numbers are also kept for reference, parallel IO performance reasons).

## performance

The first performance consideration is minimising "rework"; i.e. doing the same thing more than once, this is largely achieved by implementing zero copy methodologies, mmap(), using casts (after validation) to access data structures stored in the index files, using a zero copy skiplist (that doesn't store the actual data, in this case the file entries in the index), and this also allows an update skiplist to share file entries with the current skiplist where they are the same and only store the differences in the update skiplist (again saving rework, disk IO, extra hashing, etc and it also means that updating the index is as simple as writing the new index to a temporary index file, closing off the old index file and then renaming the new index file over the old one).

Due to the ordered nature of the index file (when in non shared, non transitory state) and always in an ordered state in skiplists (regardless of state or process) we can make extensive use of the Hwang-Lin algorithm to check for changes / updates / merges; this greatly enhances performance.

And lastly the zerocopyskiplist implementation also provides an efficient `Pwritev()` compatible function that can be used to make an IO efficient full dump of an index file (especially useful for updates / merges of index files).

## dircache layout

This index is saved in a `.dcfh` directory (in a similar fashion to git) and the current index file is called `main.idx` in the `.dcfh` directory. A cache index file called `cache.idx` is a sparse index file that may or may not be in pathname order on disk (but ordered via a skiplist after it is loaded; see index file format below for more details) and is used to temporarily cache any changes on disk that haven't been updated into the main `main.idx` file; e.g. via the `dcfh update` command. Temporary index files are called `tmp-<pid>-<tid>.idx` (process id and thread id) to make sure there are no collisions. Scan index files are called `scan-<pid>-<tid>.idx` and are used during directory scanning operations. The `.dcfh` directory also has a `.dcfhignore` file that uses golang `regexp` to ignore, or to unignore a subset of an ignore, files from the index(es).

When creating a new dircache directory (`.dcfh`) via `NewDirectoryCache` (i.e., `dcfh init`), an empty main index file is created rather than performing an initial scan. The actual file scanning and indexing occurs during subsequent operations.

## index file format

There are two types of index files; a full index that contains an entry for all specified files / directories (including their contents and sub directory contents, remember an index only contains entries for "normal" files, while excluding ignored files) in a dircache, or a sparse index that only contains some entries in a dircache. Sparse index files contain only entries that have changed since the last update or status command, and may or may not be stored in pathname order on disk. These are typically used for updates so that an update to an index can "share" entries from a main index file in a temporary sparse index file. The sparse index files are typically used by a skiplist to keep an updated, new, or deleted entry before a merge, or to cache changes to files on disk compared to the main index. A sparse file is indicated by the `Sparse` flag set in the index header `Flag` field.

Sparse index files may contain entries with a `Deleted` flag set and an empty `Hash` field to indicate that a file that exists in the main index has since been deleted. These deleted entries are used to update the temporary update skiplist and will be filtered out when the new main index is written, effectively removing the deleted files from the index.

## zero copy skiplist

The zero copy skiplist (https://github.com/mattkeenan/zerocopyskiplist.git) is used to efficiently provide a searchable, insertable, updateable ordered list of file entries for the index. Full index files are already ordered by pathname, but sparse files are generally only partially ordered and/or have a partial set of entries, typically relying on using pointers from other full index files (and their skiplists) for files that haven't changed.

All skiplists (regardless of whether used with a full index or sparse index) use the context feature to record which index file the entry is saved in. This context tracking ensures that entries remain properly mapped to prevent segmentation faults when accessing mmap'd memory regions, and provides essential metadata about the source of each entry during merge operations.

The zero copy skiplist is also used to merge two skiplists, this is primarily used by the `dcfh update` functionality and cache management operations.

### update of the main index using skiplists

A copy of the main index skiplist is made, this copy is then merged with the on disk sparse `cache.idx`, and then a scan of all the files and directories is made in a temporary sparse scan index file, and then the skiplist for this temporary scan index file is merged into the initial copy of the skiplist, and then this updated copy of the skiplist (which now has up to date entries for all the files) is then used via `Pwritev()` to efficiently write all the entries (using gather writes from the various source entries in the old full index, cache index, and scan index files) into a new in order full index. 

During this process, entries with the `Deleted` flag are filtered out to ensure deleted files are properly removed from the new main index. Once this new full in order index is written to disk then it is renamed over the old full index file and the update is complete, then the no longer needed `cache.idx` file and any temporary index files are deleted (as they are now out of date).

### index cache management for status and dupes operations

we really should avoid building the cache from scratch wherever possible; the whole point of the cache is to persistently store changes and hash calculations between uses. the cache.idx file should also NOT have entries that are in the main index file; it should be an "exclusive cache", when the cache.idx file is updated the skiplist (after it is merged) should filter out entries that are in the main index file, this can be done because the skiplist context should be tracking which file the entry is stored in (remember the a copy of the main index is made first (setting those entries with a context of the main index file), then the current cache.idx skiplist is merged (with entries whose context points to the cache.idx file), and then a scan is done with a new temporary skiplist with entries in a temporary index file with the context pointing to that temporary index file, and the skiplist for the temporary scan index file is then merged into the copy of the main index skiplist. finally that fulled merged skiplist is then written to a new temporary index cache file by filtering out any entries that have a context pointing to the main index file (i.e. all entries that aren't already in the main index), this temporary file is then renamed over the old cache.idx file which completes the update of the new cache.idx file.

When a `status` or `dupes` command is run, the system updates the cache index (but not the main index) through the following process:

1. Copy the main index skiplist
2. Merge with the existing `cache.idx` skiplist if it exists  
3. Create a new skiplist by scanning the dcfh root directory
4. Merge the scan results with the cache skiplist
5. Filter the resulting skiplist to include only entries that are not present in the main index
6. Write these filtered entries to a new temporary `cache.idx` file
7. Atomically rename the new cache file over the existing `cache.idx`

This approach ensures that `status` and `dupes` operations always work with current data while maintaining performance by avoiding updates to the main index file for read-only operations. The cache index serves as a staging area for tracking changes without the overhead of full index rebuilds.

## main process workflows

### initialisation process (dcfh init)

```
FUNCTION InitializeDirectoryCache(rootDirectory)
    // Validate input directory
    IF NOT DirectoryExists(rootDirectory) THEN
        RETURN Error("Directory does not exist")
    
    // Check if already initialized
    dcfhPath = Join(rootDirectory, ".dcfh")
    IF DirectoryExists(dcfhPath) THEN
        RETURN Error("Directory already initialized")
    
    // Create .dcfh directory structure
    CreateDirectory(dcfhPath)
    
    // Create empty main index file (no scanning yet)
    indexPath = Join(dcfhPath, "index")
    CreateEmptyIndexFile(indexPath)
    
    // Initialize ignore patterns file
    ignorePath = Join(dcfhPath, "ignore")
    CreateEmptyIgnoreFile(ignorePath)
    
    // Create and return directory cache instance
    cache = NewDirectoryCache(rootDirectory, dcfhPath)
    RETURN cache
END FUNCTION
```

### status process (dcfh status)

```
FUNCTION GetDirectoryStatus(cache)
    // Load main index into skiplist with context tracking
    mainSkiplist = LoadMainIndex(cache.indexPath)
    
    // Update cache index with current state
    UpdateCacheIndex(cache, mainSkiplist)
    
    // Load updated cache index
    cacheSkiplist = LoadCacheIndex(cache.cachePath)
    
    // Merge main and cache for complete current state
    workingSkiplist = Copy(mainSkiplist)
    Merge(workingSkiplist, cacheSkiplist)
    
    // Scan current directory state
    currentSkiplist = ScanDirectoryToSkiplist(cache.rootDir)
    
    // Use Hwang-Lin algorithm to compare states
    statusResult = HwangLinCompare(workingSkiplist, currentSkiplist)
    
    RETURN statusResult  // Contains: modified, added, deleted lists
END FUNCTION

FUNCTION UpdateCacheIndex(cache, mainSkiplist)
    // Step 1: Copy main index skiplist
    tempSkiplist = Copy(mainSkiplist)
    
    // Step 2: Merge existing cache if present
    IF FileExists(cache.cachePath) THEN
        existingCache = LoadCacheIndex(cache.cachePath)
        Merge(tempSkiplist, existingCache)
    
    // Step 3: Scan current directory state
    scanSkiplist = ScanDirectoryToSkiplist(cache.rootDir)
    
    // Step 4: Merge scan results
    Merge(tempSkiplist, scanSkiplist)
    
    // Step 5: Filter to only non-main-index entries
    cacheOnlyEntries = FilterNonMainEntries(tempSkiplist, mainSkiplist)
    
    // Step 6: Write new cache index
    tempCachePath = GenerateTempFileName("cache", GetPID(), GetThreadID())
    WriteSparseIndex(cacheOnlyEntries, tempCachePath)
    
    // Step 7: Atomic rename
    AtomicRename(tempCachePath, cache.cachePath)
END FUNCTION
```

### update process (dcfh update)

```
FUNCTION UpdateDirectoryIndex(cache, pathsToUpdate)
    // Load main index
    mainSkiplist = LoadMainIndex(cache.indexPath)
    
    // Step 1: Copy main index skiplist with context
    updateSkiplist = Copy(mainSkiplist)
    
    // Step 2: Merge with cache index if exists
    IF FileExists(cache.cachePath) THEN
        cacheSkiplist = LoadCacheIndex(cache.cachePath)
        Merge(updateSkiplist, cacheSkiplist)
    
    // Step 3: Scan specified paths (or all if none specified)
    IF pathsToUpdate IS Empty THEN
        scanPaths = [cache.rootDir]
    ELSE
        scanPaths = pathsToUpdate
    
    scanSkiplist = ScanPathsToSkiplist(scanPaths)
    
    // Step 4: Merge scan results
    Merge(updateSkiplist, scanSkiplist)
    
    // Step 5: Filter out deleted entries for final index
    finalSkiplist = FilterDeletedEntries(updateSkiplist)
    
    // Step 6: Write new main index atomically
    tempIndexPath = GenerateTempFileName("index", GetPID(), GetThreadID())
    WriteCompleteIndex(finalSkiplist, tempIndexPath)
    
    // Step 7: Atomic operations
    AtomicRename(tempIndexPath, cache.indexPath)
    
    // Step 8: Cleanup - remove cache since it's now incorporated
    RemoveFile(cache.cachePath)
    CleanupTempFiles(cache.dcfhPath)
    
    // Step 9: Reload for immediate use
    cache.skiplist = LoadMainIndex(cache.indexPath)
END FUNCTION
```

### dupes process (dcfh dupes)

```
FUNCTION FindDuplicateFiles(cache)
    // Load main index
    mainSkiplist = LoadMainIndex(cache.indexPath)
    
    // Update cache index (same as status)
    UpdateCacheIndex(cache, mainSkiplist)
    
    // Load updated cache index
    cacheSkiplist = LoadCacheIndex(cache.cachePath)
    
    // Merge main and cache for complete current state
    workingSkiplist = Copy(mainSkiplist)
    Merge(workingSkiplist, cacheSkiplist)
    
    // Build hash map for duplicate detection
    hashGroups = NewHashMap()
    
    FOR EACH entry IN workingSkiplist DO
        IF NOT entry.IsDeleted() THEN
            hash = entry.HashString()
            AddToGroup(hashGroups[hash], entry)
    
    // Filter to only groups with multiple files
    duplicateGroups = NewMap()
    FOR EACH hash, entries IN hashGroups DO
        IF Length(entries) > 1 THEN
            duplicateGroups[hash] = entries
    
    RETURN duplicateGroups
END FUNCTION
```

### helper functions

```
FUNCTION GenerateTempFileName(prefix, pid, tid)
    RETURN prefix + "-" + ToString(pid) + "-" + ToString(tid) + ".tmp"
END FUNCTION

FUNCTION FilterNonMainEntries(combinedSkiplist, mainSkiplist)
    result = NewSkiplist()
    FOR EACH entry IN combinedSkiplist DO
        IF NOT ExistsInSkiplist(mainSkiplist, entry.RelativePath()) THEN
            Insert(result, entry)
    RETURN result
END FUNCTION

FUNCTION FilterDeletedEntries(skiplist)
    result = NewSkiplist()
    FOR EACH entry IN skiplist DO
        IF NOT entry.IsDeleted() THEN
            Insert(result, entry)
    RETURN result
END FUNCTION

FUNCTION HwangLinCompare(indexSkiplist, diskSkiplist)
    // Implementation of Hwang-Lin merge algorithm for comparison
    modified = []
    added = []
    deleted = []
    
    indexEntries = GetSortedEntries(indexSkiplist)
    diskEntries = GetSortedEntries(diskSkiplist)
    
    i = 0, j = 0
    WHILE i < Length(indexEntries) AND j < Length(diskEntries) DO
        cmp = Compare(indexEntries[i].path, diskEntries[j].path)
        
        IF cmp == 0 THEN  // Same file
            IF FileMetadataChanged(indexEntries[i], diskEntries[j]) THEN
                Append(modified, indexEntries[i].path)
            i++, j++
        ELSE IF cmp < 0 THEN  // File in index but not on disk
            Append(deleted, indexEntries[i].path)
            i++
        ELSE  // File on disk but not in index
            Append(added, diskEntries[j].path)
            j++
    
    // Handle remaining entries
    WHILE i < Length(indexEntries) DO
        Append(deleted, indexEntries[i].path)
        i++
    
    WHILE j < Length(diskEntries) DO
        Append(added, diskEntries[j].path)
        j++
    
    RETURN StatusResult{modified, added, deleted}
END FUNCTION
```
