package dircachefilehash

// Index is the in-memory result of loading one .idx file: the mmap'd
// bytes (refcounted), the parsed binaryEntryRef slice that points into
// those bytes, and the stat identity used by the memo's invalidation
// check.
//
// Lifetime: an Index holds one ref on its File. The owning code path
// (the read-only mmap memo, or a one-shot load via openFileRef) is
// responsible for the matching DecRef when the Index is no longer
// needed — typically via release() during memo drain.
type Index struct {
	File *mmapIndexFile
	Refs []binaryEntryRef
	Stat cachedStat
}

// release DecRefs the underlying mmap. Safe on a nil receiver and on
// an Index whose File was never set.
func (idx *Index) release() {
	if idx == nil || idx.File == nil {
		return
	}
	idx.File.DecRef()
}
