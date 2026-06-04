package dircachefilehash

import (
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// binaryEntry is the canonical on-disk entry. Its definition, methods, and
// layout assertions now live in pkg/format (the single owner of the on-disk
// layout); this alias keeps the existing core references and method calls
// working unchanged.
type binaryEntry = format.Entry

// BESizeFromPathLen forwards to pkg/format, the owner of layout sizing.
func BESizeFromPathLen(pathLen int) int {
	return format.BESizeFromPathLen(pathLen)
}

// binaryEntryRef represents an offset-based reference to a binaryEntry in mmap'd memory.
// It stays in the core package because it references the core mmapIndexFile type;
// it is mremap-safe since it uses offsets instead of raw pointers.
type binaryEntryRef struct {
	Offset    int            // Offset from start of entry data (after header)
	IndexFile *mmapIndexFile // Reference to the mmap'd index file
}

// GetBinaryEntry resolves the reference to get the actual binaryEntry pointer
func (ref *binaryEntryRef) GetBinaryEntry() *binaryEntry {
	if ref.IndexFile == nil {
		if IsDebugEnabled("load") {
			VerboseLog(3, "GetBinaryEntry: IndexFile is nil")
		}
		return nil
	}

	// Read lock to protect against concurrent mremap operations
	ref.IndexFile.mutex.RLock()
	defer ref.IndexFile.mutex.RUnlock()

	if ref.IndexFile.Data == nil {
		if IsDebugEnabled("load") {
			VerboseLog(3, "GetBinaryEntry: IndexFile.Data is nil, IndexFile=%p, FilePath=%s", ref.IndexFile, ref.IndexFile.FilePath)
		}
		return nil
	}

	if IsDebugEnabled("load") {
		VerboseLog(3, "GetBinaryEntry: offset=%d, data_size=%d", ref.Offset, len(ref.IndexFile.Data))
	}

	// Resolve via unsafe.Add so pointer provenance is preserved (checkptr-clean):
	// the result is based on &Data[0], headerSize+Offset bytes in.
	entryPtr := unsafe.Add(unsafe.Pointer(&ref.IndexFile.Data[0]), ref.IndexFile.headerSize+ref.Offset)
	return (*binaryEntry)(entryPtr)
}

// createBinaryEntryRef creates a binaryEntryRef from a binaryEntry pointer and mmapIndexFile
func createBinaryEntryRef(entry *binaryEntry, indexFile *mmapIndexFile) binaryEntryRef {
	if indexFile == nil {
		return binaryEntryRef{}
	}

	// Read lock to protect against concurrent mremap operations
	indexFile.mutex.RLock()
	defer indexFile.mutex.RUnlock()

	if indexFile.Data == nil {
		return binaryEntryRef{}
	}

	// Calculate offset from base of entry data (after header)
	entryPtr := uintptr(unsafe.Pointer(entry))
	basePtr := uintptr(unsafe.Pointer(&indexFile.Data[0])) + uintptr(indexFile.headerSize) //nolint:gosec // G115: header size, non-negative and bounded on 64-bit
	offset := int(entryPtr - basePtr)                                                      //nolint:gosec // G115: offset within the mmap'd file, bounded by file size on 64-bit

	return binaryEntryRef{
		Offset:    offset,
		IndexFile: indexFile,
	}
}
