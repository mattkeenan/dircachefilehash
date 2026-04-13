package dircachefilehash

import "unsafe"

// PipelineOp classifies what happened to a file during comparison.
type PipelineOp uint8

const (
	// OpUnchanged means the file exists in both iterators with identical metadata.
	OpUnchanged PipelineOp = iota
	// OpModified means the file exists in both iterators but metadata differs.
	OpModified
	// OpNewFile means the file exists only in the scan (right) iterator.
	OpNewFile
	// OpDeleted means the file exists only in the existing (left) iterator.
	OpDeleted
)

// PipelineEntry carries an entry through all pipeline stages.
// Ownership transfers with the entry — only one goroutine holds it at a time.
type PipelineEntry struct {
	Entry     BinaryEntryInterface
	SeqNum    uint64     // assigned at comparison stage, monotonically increasing
	Operation PipelineOp // what happened to this file
	NeedsHash bool
	// WriteData holds the serialised bytes for this entry. Set before the write
	// stage and kept alive until after writev() completes, preventing the GC from
	// collecting backing memory while Iovec.Base still references it.
	WriteData []byte
}

// markSerialisedDeleted sets the deleted flag (bit 0 of EntryFlags) on a
// serialised binaryEntry byte slice. The data must be a heap-allocated copy,
// not a pointer into mmap'd memory.
func markSerialisedDeleted(data []byte) {
	if len(data) < int(unsafe.Sizeof(binaryEntry{})) {
		return
	}
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	entry.EntryFlags |= 1
}

// ComparisonSink receives comparison results from hwangLinUnified.
// This is the only interface the comparison algorithm needs.
type ComparisonSink interface {
	// OnMatch is called when both iterators have an entry with the same path.
	OnMatch(left, right BinaryEntryInterface) error
	// OnLeftOnly is called for entries present only in the left (existing) iterator.
	OnLeftOnly(entry BinaryEntryInterface) error
	// OnRightOnly is called for entries present only in the right (scan) iterator.
	OnRightOnly(entry BinaryEntryInterface) error
	// Close signals that no more entries will arrive. Implementations should
	// close their output channels.
	Close() error
}

// EntrySerialiser converts a BinaryEntryInterface into wire-format bytes
// suitable for writing to an index file.
type EntrySerialiser interface {
	Serialise(entry BinaryEntryInterface) ([]byte, error)
}
