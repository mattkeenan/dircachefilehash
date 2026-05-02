package dircachefilehash

import (
	"fmt"
	"unsafe"
)

// defaultEntrySerialiser implements EntrySerialiser for all BinaryEntryInterface types.
//
// Serialisation strategy per entry type:
//   - BEScanEntry: returns the existing binaryData slice directly (already correctly sized)
//   - BESkiplistEntry: copies entry.Size bytes from mmap to a new heap allocation
//   - Fallback: allocates a buffer and fills it field-by-field via the interface
//
// The returned []byte must be kept alive until after any writev() call that
// references it completes.
type defaultEntrySerialiser struct{}

// NewEntrySerialiser returns a new EntrySerialiser.
func NewEntrySerialiser() EntrySerialiser {
	return &defaultEntrySerialiser{}
}

// Serialise converts a BinaryEntryInterface to wire-format bytes.
func (s *defaultEntrySerialiser) Serialise(entry BinaryEntryInterface) ([]byte, error) {
	// Fast path: BEScanEntry already has a contiguous buffer
	if scanEntry, ok := entry.(*BEScanEntry); ok {
		return serialiseScanEntry(scanEntry)
	}

	// Fast path: BESkiplistEntry — copy from mmap using actual entry size
	if skiplistEntry, ok := entry.(*BESkiplistEntry); ok {
		return serialiseSkiplistEntry(skiplistEntry)
	}

	// Fallback: build from interface methods
	return serialiseFromInterface(entry)
}

// serialiseScanEntry returns the existing heap-allocated buffer.
// The caller must keep the returned slice (and by extension the BEScanEntry)
// alive until the write is complete.
func serialiseScanEntry(sbe *BEScanEntry) ([]byte, error) {
	data, err := sbe.GetBinaryData()
	if err != nil {
		return nil, fmt.Errorf("failed to get binary data from scan entry: %w", err)
	}
	return data, nil
}

// serialiseSkiplistEntry copies the variable-length entry from mmap memory
// into a new heap-allocated buffer. This decouples the write data from the
// mmap lifetime, eliminating the class of bugs where mmap pointers are used
// directly in Iovecs.
func serialiseSkiplistEntry(sle *BESkiplistEntry) ([]byte, error) {
	// Acquire read lock on index for mremap safety
	if sle.entryRef.IndexFile != nil {
		sle.entryRef.IndexFile.mutex.RLock()
		defer sle.entryRef.IndexFile.mutex.RUnlock()
	}

	be := sle.entryRef.GetBinaryEntry()
	if be == nil {
		return nil, ErrEntryInvalidated
	}

	// Use the actual entry Size field (includes variable-length path + padding),
	// NOT unsafe.Sizeof(binaryEntry{}) which is just the fixed struct size.
	entrySize := int(be.Size)
	if entrySize <= 0 {
		return nil, fmt.Errorf("invalid entry size %d", entrySize)
	}

	// Copy from mmap into heap-allocated buffer
	src := unsafe.Slice((*byte)(unsafe.Pointer(be)), entrySize)
	dst := make([]byte, entrySize)
	copy(dst, src)

	return dst, nil
}

// serialiseFromInterface builds wire-format bytes field-by-field from the
// BinaryEntryInterface methods. This is the slowest path but works for any
// implementation.
func serialiseFromInterface(entry BinaryEntryInterface) ([]byte, error) {
	entrySize, err := entry.Size()
	if err != nil {
		return nil, fmt.Errorf("failed to get entry size: %w", err)
	}
	if entrySize == 0 {
		return nil, fmt.Errorf("entry has zero size")
	}

	data := make([]byte, entrySize)
	be := (*binaryEntry)(unsafe.Pointer(&data[0]))

	be.Size = entrySize

	if be.CTimeWall, err = entry.CTimeWall(); err != nil {
		return nil, err
	}
	if be.MTimeWall, err = entry.MTimeWall(); err != nil {
		return nil, err
	}
	if be.Dev, err = entry.Dev(); err != nil {
		return nil, err
	}
	if be.Ino, err = entry.Ino(); err != nil {
		return nil, err
	}
	if be.Mode, err = entry.Mode(); err != nil {
		return nil, err
	}
	if be.UID, err = entry.UID(); err != nil {
		return nil, err
	}
	if be.GID, err = entry.GID(); err != nil {
		return nil, err
	}
	if be.FileSize, err = entry.FileSize(); err != nil {
		return nil, err
	}
	if be.HashType, err = entry.HashType(); err != nil {
		return nil, err
	}

	hash, err := entry.Hash()
	if err != nil {
		return nil, err
	}
	copy(be.Hash[:], hash[:])

	flags, err := entry.EntryFlags()
	if err != nil {
		return nil, err
	}
	be.EntryFlags = uint16(flags)

	relPath, err := entry.RelativePath()
	if err != nil {
		return nil, err
	}

	// Path starts after the struct (matching RelativePath)
	pathOffset := int(unsafe.Sizeof(*be))
	pathSpace := data[pathOffset:]
	pathBytes := []byte(relPath)
	copy(pathSpace, pathBytes)
	if len(pathSpace) > len(pathBytes) {
		pathSpace[len(pathBytes)] = 0
	}

	return data, nil
}
