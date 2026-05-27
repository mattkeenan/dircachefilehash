package dircachefilehash

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
	"golang.org/x/sys/unix"
)

// indexHeader is the canonical on-disk header. Its definition and methods now
// live in pkg/format (the single owner of the on-disk layout); this alias keeps
// existing core references and method calls working unchanged.
type indexHeader = format.Header

// headerSizeForVersion forwards to pkg/format, the owner of layout sizing.
func headerSizeForVersion(version uint32) int {
	return format.HeaderSizeForVersion(version)
}

// HeaderSizeForVersion forwards to pkg/format (exported for dcfhfix).
func HeaderSizeForVersion(version uint32) int {
	return format.HeaderSizeForVersion(version)
}

// mmapIndex represents a memory-mapped index file
type mmapIndex struct {
	data []byte
	file *os.File
}

// mmapIndexFile represents a wrapper for index file lifecycle management
type mmapIndexFile struct {
	File       *os.File     // File descriptor (nil for read-only main/cache indices)
	Data       []byte       // Memory-mapped data
	Size       int          // Current size of the mapping
	Offset     int          // Current write offset for scan indices
	Type       string       // Index type: "main", "cache", "scan"
	FilePath   string       // File path for debugging/cleanup
	headerSize int          // Version-dependent header size (V2HeaderSize or HeaderSize)
	heapBacked bool         // Data is a GC'd transcode buffer (legacy load), not an mmap — never munmap it
	mutex      sync.RWMutex // Protects Data/Size during mremap operations
	refCount   int32        // Atomic reference counter for safe cleanup
}

// Cleanup safely unmaps and closes the index file
func (mif *mmapIndexFile) Cleanup() error {
	mif.mutex.Lock()
	defer mif.mutex.Unlock()

	// heapBacked Data is a Go-allocated transcode image (legacy v2/v3 load), not
	// a mapping — calling munmap on it is undefined behaviour. The marker, not a
	// nil fd, is the discriminator: read-only main/cache indices also have File==nil.
	if mif.Data != nil && !mif.heapBacked {
		if err := unix.Munmap(mif.Data); err != nil {
			return fmt.Errorf("failed to unmap %s index: %w", mif.Type, err)
		}
	}
	// Drop the reference for both cases: an mmap is now unmapped, and a heap
	// image is released so the GC can reclaim it.
	mif.Data = nil

	if mif.File != nil {
		if err := mif.File.Close(); err != nil {
			return fmt.Errorf("failed to close %s index file: %w", mif.Type, err)
		}
		mif.File = nil
	}

	return nil
}

// IncRef atomically increments the reference count
func (mif *mmapIndexFile) IncRef() {
	atomic.AddInt32(&mif.refCount, 1)
}

// DecRef atomically decrements the reference count and cleans up if it reaches zero
func (mif *mmapIndexFile) DecRef() {
	if atomic.AddInt32(&mif.refCount, -1) == 0 {
		// Last reference released - safe to cleanup
		if err := mif.Cleanup(); err != nil {
			// Log error but don't return it since DecRef() should not fail
			if IsDebugEnabled("load") {
				VerboseLog(2, "Warning: cleanup failed during DecRef for %s: %v", mif.Type, err)
			}
		}
	}
}

// RefCount returns the current reference count (for debugging)
func (mif *mmapIndexFile) RefCount() int32 {
	return atomic.LoadInt32(&mif.refCount)
}

// Header returns a direct pointer to the header in mmap'd memory (zero-copy)
func (mi *mmapIndex) Header() *indexHeader {
	return (*indexHeader)(unsafe.Pointer(&mi.data[0]))
}

// Header validation methods (ValidateSignature, ValidateVersion, ValidateByteOrder)
// now live in pkg/format on the Header type; callers reach them via the indexHeader alias.

// ValidateIndexHeader validates an index file header and returns a copy of the header struct
// This is a shared utility function that can be used across the codebase for header validation
func ValidateIndexHeader(indexPath string, validateVersion bool, expectedVersion uint32) (*indexHeader, error) {
	return ValidateIndexHeaderWithOptions(indexPath, validateVersion, expectedVersion, true)
}

// ValidateIndexHeaderWithOptions validates index header with configurable checksum validation
func ValidateIndexHeaderWithOptions(indexPath string, validateVersion bool, expectedVersion uint32, validateChecksum bool) (*indexHeader, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Mmap enough for a v3 header. For v2 files smaller than HeaderSize,
	// mmap the file size — POSIX zero-fills beyond EOF for MAP_PRIVATE.
	mmapSize := HeaderSize
	if stat.Size() < int64(mmapSize) {
		mmapSize = int(stat.Size())
	}

	// Memory map the header for reading
	data, err := unix.Mmap(int(file.Fd()), 0, mmapSize, unix.PROT_READ, unix.MAP_PRIVATE) //nolint:gosec // G115: file descriptor (uintptr) to int, bounded on 64-bit
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file header: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

	// Get direct pointer to header in mmap'd memory (zero-copy).
	// For v2 files where mmapSize < sizeof(indexHeader), the Timestamp field
	// at the end of the struct may read beyond the mmap. This is safe because
	// mmap rounds up to page size, and those bytes read as zero.
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header using the standard validation methods
	signature := [4]byte{'d', 'c', 'f', 'h'}
	if err := header.ValidateSignature(signature); err != nil {
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		return nil, err
	}
	if validateVersion {
		if err := header.ValidateVersion(expectedVersion); err != nil {
			return nil, err
		}
	}

	// Check Clean flag to determine if we should trust the header checksum
	isClean := (header.Flags & IndexFlagClean) != 0

	if validateChecksum && !isClean {
		// File wasn't closed cleanly - header checksum is likely incorrect
		// Skip checksum validation for recovery purposes
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", indexPath)
	} else if validateChecksum && isClean {
		// File was closed cleanly - validate the header checksum
		if err := validateHeaderChecksum(file, header, stat.Size()); err != nil {
			return nil, fmt.Errorf("header checksum validation failed: %w", err)
		}
	}

	// Create a copy of the header since we're unmapping the memory
	headerCopy := *header
	return &headerCopy, nil
}

// validateHeaderChecksum validates the header checksum against the file contents
func validateHeaderChecksum(file *os.File, header *indexHeader, fileSize int64) error {
	// Calculate expected checksum using same order as TempIndexWriter iterative approach
	hasher := sha1.New()

	// Entry data starts after the version-specific header
	hdrSize := int64(headerSizeForVersion(header.Version))

	// First: Hash entry data (matches TempIndexWriter.WriteIoVecBatch order)
	entryDataSize := fileSize - hdrSize
	if entryDataSize > 0 {
		// Read entry data
		entryData := make([]byte, entryDataSize)
		if _, err := file.ReadAt(entryData, hdrSize); err != nil {
			return fmt.Errorf("failed to read entry data for checksum validation: %w", err)
		}
		hasher.Write(entryData)
	}

	// Second: Hash header up to checksum field (matches TempIndexWriter.Close order).
	// checksumOffset is the same for v2 and v3 (Timestamp is after Checksum).
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	// Compare with stored checksum
	expectedChecksum := hasher.Sum(nil)
	if !bytes.Equal(expectedChecksum, header.Checksum[:len(expectedChecksum)]) {
		return fmt.Errorf("checksum mismatch: expected %x, got %x", expectedChecksum, header.Checksum[:len(expectedChecksum)])
	}

	return nil
}

// Header initialisation methods (SetHeader, SetHeaderForWritableIndex) now live
// in pkg/format on the Header type; callers reach them via the indexHeader alias.

// calculateAndStoreHeaderChecksum calculates checksum and stores it in header
func (ms *MetaStore) calculateAndStoreHeaderChecksum(header *indexHeader, entryData []byte, entrySize int) {
	hasher := ms.hasher
	hasher.Reset()

	// IMPORTANT: Use same order as TempIndexWriter: entry data first, then header fields

	// First: Hash entry data if any (matches TempIndexWriter.WriteIoVecBatch order)
	if entrySize > 0 {
		hasher.Write(entryData[:entrySize])
	}

	// Second: Hash header up to checksum field (matches TempIndexWriter.addHeaderToChecksum order)
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	// Store checksum in header
	checksumBytes := hasher.Sum(nil)
	copy(header.Checksum[:], checksumBytes)
}

// Clean-bit methods (IsClean, SetClean, ClearClean) now live in pkg/format on
// the Header type; callers reach them via the indexHeader alias.

// EntryProcessor defines a callback function for processing entries during index loading
// Parameters: entry (the binaryEntry), entryIndex (0-based), filePath (source file)
// Returns: shouldInclude (whether to include in result), error (if processing failed)
type EntryProcessor func(entry *binaryEntry, entryIndex uint32, filePath string) (shouldInclude bool, err error)

// LoadIndexFromFileForValidation is a public wrapper for loadIndexFromFile used by dcfh index commands
func (ms *MetaStore) LoadIndexFromFileForValidation(filePath string) ([]binaryEntryRef, error) {
	// Use verbose processor for validation operations to maintain existing behaviour
	return ms.loadIndexFromFileWithProcessor(filePath, VerboseEntryProcessor())
}

// LoadIndexFromFileWithProcessor loads an index file with custom entry processing
func (ms *MetaStore) LoadIndexFromFileWithProcessor(filePath string, processor EntryProcessor) ([]binaryEntryRef, error) {
	return ms.loadIndexFromFileWithProcessor(filePath, processor)
}

// loadIndexFromFileWithProcessor is the internal implementation with callback support
func (ms *MetaStore) loadIndexFromFileWithProcessor(filePath string, processor EntryProcessor) ([]binaryEntryRef, error) {
	indexFile, header, err := ms.openAndValidateIndex(filePath)
	if err != nil {
		return nil, err
	}
	return ms.collectEntryRefs(indexFile, header, filePath, processor)
}

// checkEntryRegionAccess gates access to an index's entry region for a header
// that has already passed the signature/byte-order/version-validate triple. It
// is the single owner of the "is this entry region safe to materialise" decision
// for every mmap entry-walk loader:
//   - version dispatch: an unsupported version is rejected via the format
//     resolver (the only real version gate for the dcfhfind validation path,
//     where ValidateVersion is a no-op for any on-disk version);
//   - header-size bounds: a v3 header on an 88..103-byte file passes the
//     V2HeaderSize size gate but would over-read when the caller slices the
//     entry region — fail closed first (NFR5: never panic on a truncated index).
//
// It returns the DecodeStrategy for the version so the caller knows whether the
// entry region can be cast in place (DecodeZeroCopy, current layout) or must be
// transcoded into a v4 heap image first (DecodeHeap, legacy v2/v3 layout).
//
// The error is fully formed before return, so a caller may munmap the backing
// data immediately afterwards without a use-after-free in the message.
func checkEntryRegionAccess(header *indexHeader, fileSize int64) (format.DecodeStrategy, error) {
	strategy, err := format.StrategyForVersion(header.Version)
	if err != nil {
		return format.DecodeReject, err
	}
	if hdrSize := headerSizeForVersion(header.Version); int64(hdrSize) > fileSize {
		return format.DecodeReject, fmt.Errorf("file too small for v%d header: %d bytes < %d",
			header.Version, fileSize, hdrSize)
	}
	return strategy, nil
}

// openAndValidateIndex opens the file, mmaps it, and runs the fixed
// header checks (signature, byte order, version, clean-flag checksum).
// On error it cleans up the fd/mmap; on success the caller owns the
// returned indexFile and must close+munmap it when done.
func (ms *MetaStore) openAndValidateIndex(filePath string) (*mmapIndexFile, *indexHeader, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open index file %s: %w", filePath, err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}
	if stat.Size() < int64(V2HeaderSize) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE) //nolint:gosec // G115: file descriptor (uintptr) to int, bounded on 64-bit
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	cleanup := func() { _ = unix.Munmap(data); _ = file.Close() }
	for _, check := range []func() error{
		func() error { return header.ValidateSignature(ms.signature) },
		func() error { return header.ValidateByteOrder() },
		func() error { return header.ValidateVersion(ms.version) },
	} {
		if err := check(); err != nil {
			cleanup()
			return nil, nil, err
		}
	}

	// Gate version dispatch + header-size bounds before collectEntryRefs slices
	// the entry region. This is where the dcfhfind validation path (version:0)
	// gets its only real version gate.
	strategy, err := checkEntryRegionAccess(header, stat.Size())
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	// Verify the checksum on the ORIGINAL on-disk bytes (before any transcode),
	// so a legacy file's integrity is checked against what it actually stored.
	if (header.Flags & IndexFlagClean) == 0 {
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", filePath)
	} else if err := ms.verifyHeaderChecksum(data, header); err != nil {
		return nil, nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	// Legacy (v2/v3) layout diverges from v4: transcode the whole image into a v4
	// heap buffer and back the index with that, rather than casting old bytes as
	// a v4 Entry. The original mapping is released; the heap image is GC-managed.
	if strategy == format.DecodeHeap {
		image, terr := format.TranscodeLegacyIndex(data)
		cleanup() // release the original mmap + fd; image is an independent copy
		if terr != nil {
			return nil, nil, fmt.Errorf("legacy index transcode failed: %w", terr)
		}
		indexFile := &mmapIndexFile{
			Data:       image,
			Size:       len(image),
			Type:       "loaded",
			FilePath:   filePath,
			headerSize: HeaderSize, // image is always a v4 header
			heapBacked: true,
		}
		return indexFile, (*indexHeader)(unsafe.Pointer(&image[0])), nil
	}

	indexFile := &mmapIndexFile{
		File:       file,
		Data:       data,
		Size:       int(stat.Size()),
		Type:       "loaded",
		FilePath:   filePath,
		headerSize: headerSizeForVersion(header.Version),
	}
	return indexFile, header, nil
}

// collectEntryRefs walks the entry region, validating each entry and
// invoking the user's processor. Returns refs for entries the
// processor accepted.
func (ms *MetaStore) collectEntryRefs(indexFile *mmapIndexFile, header *indexHeader, filePath string, processor EntryProcessor) ([]binaryEntryRef, error) {
	// Precondition: openAndValidateIndex (this function's only caller) has already
	// run checkEntryRegionAccess, gating version dispatch and header-size bounds,
	// so the entry-region slice below is safe to take.
	entryData := indexFile.Data[indexFile.headerSize:]
	var refs []binaryEntryRef
	offset := 0
	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			return nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
		if err := ms.validateSingleEntry(entry, offset, entryData, int(i)); err != nil {
			return nil, err
		}
		include, err := runEntryProcessor(processor, entry, i, filePath)
		if err != nil {
			return nil, err
		}
		if include {
			refs = append(refs, binaryEntryRef{Offset: offset, IndexFile: indexFile})
		}
		nextOffset := offset + int(entry.Size)
		if IsDebugEnabled("indexchaining") && i < header.EntryCount-1 && nextOffset >= len(entryData) {
			return nil, fmt.Errorf("entry %d size %d would exceed data bounds (offset %d + size = %d, max %d)",
				i, entry.Size, offset, nextOffset, len(entryData))
		}
		offset = nextOffset
	}
	if offset != len(entryData) {
		return nil, fmt.Errorf("data size mismatch: consumed %d bytes, expected %d bytes", offset, len(entryData))
	}
	return refs, nil
}

// validateSingleEntry wraps the chaining / extra-validation checks in
// one place so collectEntryRefs stays straight-line.
func (ms *MetaStore) validateSingleEntry(entry *binaryEntry, offset int, entryData []byte, index int) error {
	if err := ms.validateEntryChaining(entry, offset, entryData, index); err != nil {
		return fmt.Errorf("entry %d validation failed: %w", index, err)
	}
	if IsDebugEnabled("extravalidation") {
		if err := entry.ValidateEntry(); err != nil {
			return fmt.Errorf("entry %d extra validation failed: %w", index, err)
		}
	}
	return nil
}

// runEntryProcessor invokes the processor (if any). Nil processor
// means "include everything".
func runEntryProcessor(processor EntryProcessor, entry *binaryEntry, index uint32, filePath string) (bool, error) {
	if processor == nil {
		return true, nil
	}
	include, err := processor(entry, index, filePath)
	if err != nil {
		return false, fmt.Errorf("entry processor failed at entry %d: %w", index, err)
	}
	return include, nil
}

// Processor factory functions for different use cases

// DefaultEntryProcessor returns a processor that includes all entries (normal loading behaviour)
func DefaultEntryProcessor() EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		return true, nil
	}
}

// VerboseEntryProcessor returns a processor that outputs verbose information based on global verbose level
func VerboseEntryProcessor() EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		entryPath := entry.RelativePath()

		if GetVerboseLevel() >= 1 {
			VerboseLog(1, "%s", entryPath) // Level 1: filename only (like 'ls')
		}
		if GetVerboseLevel() >= 2 {
			// Level 2: ls -l style output (mode, index filename, mtime, path)
			mtime := timeFromWall(entry.MTimeWall)
			VerboseLog(2, "  %04o %8d %s %s (%s)", entry.Mode&0o7777, entry.FileSize,
				mtime.Format("2006-01-02 15:04:05"), entryPath, filepath.Base(filePath))
		}
		if GetVerboseLevel() >= 3 {
			// Level 3: complete breakdown of each field in binaryEntry
			VerboseLog(3, "  Entry %d details:", entryIndex)
			VerboseLog(3, "    Size: %d bytes", entry.Size)
			VerboseLog(3, "    CTimeWall: %d (%s)", entry.CTimeWall, timeFromWall(entry.CTimeWall))
			VerboseLog(3, "    MTimeWall: %d (%s)", entry.MTimeWall, timeFromWall(entry.MTimeWall))
			VerboseLog(3, "    Dev: %d", entry.Dev)
			VerboseLog(3, "    Ino: %d", entry.Ino)
			VerboseLog(3, "    Mode: 0o%o", entry.Mode)
			VerboseLog(3, "    UID: %d", entry.UID)
			VerboseLog(3, "    GID: %d", entry.GID)
			VerboseLog(3, "    FileSize: %d", entry.FileSize)
			VerboseLog(3, "    EntryFlags: 0x%04x%s", entry.EntryFlags,
				func() string {
					if entry.IsDeleted() {
						return " (DELETED)"
					} else {
						return ""
					}
				}())
			VerboseLog(3, "    HashType: %d (%s)", entry.HashType, HashTypeName(entry.HashType))
			VerboseLog(3, "    Hash: %s", entry.HashString())
			VerboseLog(3, "    Path: %s", entryPath)
		}

		return true, nil
	}
}

// SearchEntryProcessor returns a processor that searches for matching entries
type SearchOptions struct {
	Pattern     string // Filename pattern (glob)
	PathPrefix  string // Path prefix filter
	HashPrefix  string // Hash prefix filter
	ExactSize   *int64 // Exact file size filter
	ShowDeleted bool   // Show only deleted entries
	SearchCount *int   // Pointer to counter for matches
}

func SearchEntryProcessor(opts SearchOptions) EntryProcessor {
	return func(entry *binaryEntry, _ uint32, filePath string) (bool, error) {
		matches, err := searchEntryMatches(opts, entry)
		if err != nil || !matches {
			return false, err
		}
		if opts.SearchCount != nil {
			*opts.SearchCount++
		}
		emitSearchMatch(entry, filePath)
		return false, nil // Don't include in skiplist, just process for output
	}
}

// searchEntryMatches applies the SearchEntryProcessor filter set.
// Returns (false, nil) for non-matches and (false, err) for a bad
// glob pattern; (true, nil) means the caller should emit.
func searchEntryMatches(opts SearchOptions, entry *binaryEntry) (bool, error) {
	if entry.IsDeleted() != opts.ShowDeleted {
		return false, nil
	}
	entryPath := entry.RelativePath()
	if opts.Pattern != "" {
		ok, err := filepath.Match(opts.Pattern, filepath.Base(entryPath))
		if err != nil {
			return false, fmt.Errorf("invalid pattern %s: %w", opts.Pattern, err)
		}
		if !ok {
			return false, nil
		}
	}
	if opts.PathPrefix != "" && !strings.HasPrefix(entryPath, opts.PathPrefix) {
		return false, nil
	}
	if opts.HashPrefix != "" {
		if !strings.HasPrefix(strings.ToLower(entry.HashString()), strings.ToLower(opts.HashPrefix)) {
			return false, nil
		}
	}
	if opts.ExactSize != nil && entry.FileSize != *opts.ExactSize {
		return false, nil
	}
	return true, nil
}

// emitSearchMatch writes one matched entry's summary at the current
// verbose level.
func emitSearchMatch(entry *binaryEntry, filePath string) {
	entryPath := entry.RelativePath()
	VerboseLog(0, "%s", entryPath)
	if GetVerboseLevel() >= 1 {
		mtime := timeFromWall(entry.MTimeWall)
		deletedFlag := ""
		if entry.IsDeleted() {
			deletedFlag = " (DELETED)"
		}
		VerboseLog(1, "  %04o %8d %s %s%s",
			entry.Mode&0o7777, entry.FileSize,
			mtime.Format("2006-01-02 15:04:05"),
			filepath.Base(filePath), deletedFlag)
	}
	if GetVerboseLevel() >= 2 {
		VerboseLog(2, "  Hash: %s (%s)", entry.HashString(), HashTypeName(entry.HashType))
	}
}

// CompositeEntryProcessor combines multiple processors (all must return true to include entry)
func CompositeEntryProcessor(processors ...EntryProcessor) EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		for _, processor := range processors {
			if processor != nil {
				shouldInclude, err := processor(entry, entryIndex, filePath)
				if err != nil {
					return false, err
				}
				if !shouldInclude {
					return false, nil
				}
			}
		}
		return true, nil
	}
}

// loadIndexFromFileWithTracking opens, mmaps, validates and parses an
// index file, returning a fresh *Index whose File holds one construction
// ref. Callers either hand the Index to the read-only mmap memo (which
// adopts ownership and DecRefs on drain) or DecRef themselves via
// idx.File.DecRef when finished. Stat is left zero — the memo fills it
// from its own os.Stat after this call returns.
func (ms *MetaStore) loadIndexFromFileWithTracking(filePath string) (*Index, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", filePath, err)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		_ = file.Close()
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE) //nolint:gosec // G115: file descriptor (uintptr) to int, bounded on 64-bit
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header; on failure, clean up both mmap and file
	if err := header.ValidateSignature(ms.signature); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}
	if err := header.ValidateVersion(ms.version); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}

	hdrSize := headerSizeForVersion(header.Version)
	indexFile := &mmapIndexFile{
		File:       file,
		Data:       data,
		Size:       int(stat.Size()),
		Type:       "loaded",
		FilePath:   filePath,
		headerSize: hdrSize,
		// Start with refCount=1 so existing error-cleanup DecRef paths
		// (and the read-only mmap memo's drain in Close) reach 0 and
		// trigger Cleanup. The construction ref is owned by whoever
		// receives indexFile — for memo'd loads, the MetaStore; for
		// direct callers like openFileRef, the returned closer.
		refCount: 1,
	}

	isClean := (header.Flags & IndexFlagClean) != 0

	if !isClean {
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", filePath)
	} else {
		if err := ms.verifyHeaderChecksum(data, header); err != nil {
			indexFile.DecRef()
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Gate version dispatch + header-size bounds before slicing the entry region.
	// (ValidateVersion(ms.version) above already rejects out-of-range versions on
	// this path; the shared helper keeps the version→materialisation decision
	// single-owned and adds the header-size bounds check ValidateVersion lacks.)
	strategy, err := checkEntryRegionAccess(header, stat.Size())
	if err != nil {
		indexFile.DecRef()
		return nil, err
	}

	// Legacy (v2/v3) layout diverges from v4: transcode the whole image into a v4
	// heap buffer, release the original mapping, and back the index with the heap
	// image. The transcode reads the original bytes before DecRef unmaps them.
	if strategy == format.DecodeHeap {
		image, terr := format.TranscodeLegacyIndex(data)
		indexFile.DecRef() // release the original mmap + fd
		if terr != nil {
			return nil, fmt.Errorf("legacy index transcode failed: %w", terr)
		}
		heapFile := &mmapIndexFile{
			Data:       image,
			Size:       len(image),
			Type:       "loaded",
			FilePath:   filePath,
			headerSize: HeaderSize, // image is always a v4 header
			heapBacked: true,
			refCount:   1,
		}
		heapHeader := (*indexHeader)(unsafe.Pointer(&image[0]))
		refs, perr := ms.parseTrackedEntries(heapFile, heapHeader, image[HeaderSize:])
		if perr != nil {
			heapFile.DecRef()
			return nil, perr
		}
		return &Index{File: heapFile, Refs: refs}, nil
	}

	// Parse the entry region. The helper does not own indexFile; on error we
	// release the construction ref here.
	refs, err := ms.parseTrackedEntries(indexFile, header, data[hdrSize:])
	if err != nil {
		indexFile.DecRef()
		return nil, err
	}

	return &Index{File: indexFile, Refs: refs}, nil
}

// parseTrackedEntries walks the entry region of a tracking-loaded index and
// returns one binaryEntryRef per entry. It does not own indexFile (the caller
// holds the construction ref and releases it on error), keeping the cleanup
// contract in loadIndexFromFileWithTracking rather than spread across the loop.
func (ms *MetaStore) parseTrackedEntries(indexFile *mmapIndexFile, header *indexHeader, entryData []byte) ([]binaryEntryRef, error) {
	var refs []binaryEntryRef
	offset := 0
	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			return nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}

		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		if err := ms.validateEntryChaining(entry, offset, entryData, int(i)); err != nil {
			return nil, fmt.Errorf("entry %d validation failed: %w", i, err)
		}

		if IsDebugEnabled("extravalidation") {
			if err := entry.ValidateEntry(); err != nil {
				return nil, fmt.Errorf("entry %d extra validation failed: %w", i, err)
			}
		}

		refs = append(refs, createBinaryEntryRef(entry, indexFile))

		nextOffset := offset + int(entry.Size)
		if nextOffset <= offset {
			return nil, fmt.Errorf("entry %d has invalid size %d (would not advance)", i, entry.Size)
		}

		if IsDebugEnabled("indexchaining") && i < header.EntryCount-1 && nextOffset >= len(entryData) {
			return nil, fmt.Errorf("entry %d size %d would exceed data bounds (offset %d + size = %d, max %d)",
				i, entry.Size, offset, nextOffset, len(entryData))
		}

		offset = nextOffset
	}

	if offset != len(entryData) {
		return nil, fmt.Errorf("data size mismatch: consumed %d bytes, expected %d bytes", offset, len(entryData))
	}

	return refs, nil
}

// verifyHeaderChecksum verifies the checksum stored in the header
func (ms *MetaStore) verifyHeaderChecksum(data []byte, header *indexHeader) error {
	// Get the stored checksum from header
	storedChecksum := header.Checksum[:]

	// Determine checksum algorithm from header
	var hasher hash.Hash
	var expectedSize int
	switch header.ChecksumType {
	case HashTypeSHA1:
		hasher = sha1.New()
		expectedSize = HashSizeSHA1
	case HashTypeSHA256:
		hasher = sha256.New()
		expectedSize = HashSizeSHA256
	case HashTypeSHA512:
		hasher = sha512.New()
		expectedSize = HashSizeSHA512
	default:
		return fmt.Errorf("unsupported checksum type: %d", header.ChecksumType)
	}

	// Calculate checksum of header (excluding checksum field) + entries
	// IMPORTANT: Must match TempIndexWriter order: entry data first, then header fields
	hasher.Reset()

	// Hash entry data first (matches TempIndexWriter.WriteIoVecBatch)
	hdrSize := headerSizeForVersion(header.Version)
	entryData := data[hdrSize:]
	hasher.Write(entryData)

	// Hash header fields before checksum field (matches TempIndexWriter.addHeaderToChecksum)
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	calculatedChecksum := hasher.Sum(nil)

	// Compare checksums
	for i := 0; i < expectedSize; i++ {
		if storedChecksum[i] != calculatedChecksum[i] {
			return fmt.Errorf("checksum mismatch at byte %d", i)
		}
	}
	return nil
}

// Close cleans up mmap'd resources and checks for orphaned index files
func (ms *MetaStore) Close() error {
	// Check for orphaned index files first (ignore errors during check)
	_ = ms.checkForOrphanedIndexFiles()

	// Clean up old mmapIndex if still present
	if ms.mmapIndex != nil {
		if err := unix.Munmap(ms.mmapIndex.data); err != nil {
			return fmt.Errorf("failed to unmap: %w", err)
		}
		if err := ms.mmapIndex.file.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}
		ms.mmapIndex = nil
	}

	// Drain the read-only mmap memo (owns lifetime of main/cache/snapshot
	// mappings loaded via loadIndexShared). ms.mainIndex/ms.cacheIndex are
	// non-owning per-type pointers maintained by registerIndex for the
	// memory-protection RWMutex machinery; the memo drain releases the
	// actual mappings, so we just nil out those pointers here.
	ms.loadedMu.Lock()
	for _, idx := range ms.loadedIndices {
		idx.release()
	}
	ms.loadedIndices = nil
	for _, idx := range ms.orphanIndices {
		idx.release()
	}
	ms.orphanIndices = nil
	ms.loadedMu.Unlock()

	ms.mainIndex = nil
	ms.cacheIndex = nil

	return nil
}

func (ms *MetaStore) createEmptyIndex() error {
	totalSize := HeaderSize

	file, err := os.Create(ms.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", ms.IndexFile, err)
	}
	defer func() { _ = file.Close() }()

	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED) //nolint:gosec // G115: file descriptor (uintptr) to int, bounded on 64-bit
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

	// Zero out the entire memory region first
	for i := range data {
		data[i] = 0
	}

	// Write header directly to mmap'd memory (zero-copy). The write version is
	// owned by SetHeaderForWritableIndex (CurrentIndexVersion); baseFlags=0 and
	// 0 &^ Clean == 0, so this is behaviour-equal to the prior SetHeader call.
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(ms.signature, 0, 0, ms.GetCurrentHashType()) // No flags for empty index

	// Calculate and store checksum (no entries for empty index)
	ms.calculateAndStoreHeaderChecksum(header, nil, 0)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// getSystemIOVMax returns the system's IOV_MAX limit using sysconf(_SC_IOV_MAX)
// Falls back to conservative default if sysconf fails
func getSystemIOVMax() int {
	// _SC_IOV_MAX constant for sysconf() - platform specific
	const SC_IOV_MAX = 60       // Linux value, may vary on other platforms
	const fallbackIOVMax = 1024 // Conservative default per golang/go#58623

	// Call sysconf directly using unix.Syscall (syscall 99 on Linux)
	r1, _, errno := unix.Syscall(99, uintptr(SC_IOV_MAX), 0, 0)
	if errno != 0 {
		// Fall back to conservative default if sysconf fails
		return fallbackIOVMax
	}

	iovMax := int(r1) //nolint:gosec // G115: IOV_MAX syscall result, small positive

	// Validate the result is reasonable, fall back if not
	if iovMax <= 0 || iovMax > 1<<20 { // Sanity check: between 1 and 1M
		return fallbackIOVMax
	}

	return iovMax
}

// scanForTempIndices scans the .dcfh directory for temporary index files
func (ms *MetaStore) scanForTempIndices() ([]string, error) {
	var tempFiles []string

	entries, err := os.ReadDir(ms.MetaDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Look for temporary index files with patterns:
		// - scan-{pid}-{tid}.idx (scan indices)
		// - tmp-{pid}-{tid}.idx (temp indices)
		if strings.HasPrefix(name, "scan-") && strings.HasSuffix(name, ".idx") ||
			strings.HasPrefix(name, "tmp-") && strings.HasSuffix(name, ".idx") {
			tempFiles = append(tempFiles, name)
		}
	}

	return tempFiles, nil
}

// validateEntryChaining validates the consistency of a binaryEntry's internal structure
// and its position within the mmap'd data
func (ms *MetaStore) validateEntryChaining(entry *binaryEntry, offset int, entryData []byte, entryIndex int) error {
	// Basic size validation
	if entry.Size == 0 {
		return fmt.Errorf("entry has zero size at offset %d (entry index %d)", offset, entryIndex)
	}

	minSize := uint32(unsafe.Sizeof(*entry))
	if entry.Size < minSize {
		return fmt.Errorf("entry size %d too small (minimum %d) at offset %d (entry index %d)",
			entry.Size, minSize, offset, entryIndex)
	}

	maxReasonableSize := uint32(4096) // Reasonable maximum for path + padding
	if entry.Size > maxReasonableSize {
		return fmt.Errorf("entry size %d unreasonably large (maximum %d) at offset %d (entry index %d)",
			entry.Size, maxReasonableSize, offset, entryIndex)
	}

	// Validate that the entry doesn't extend beyond available data
	if offset+int(entry.Size) > len(entryData) {
		return fmt.Errorf("entry size %d at offset %d would extend beyond data bounds (available: %d) (entry index %d)",
			entry.Size, offset, len(entryData)-offset, entryIndex)
	}

	// Validate 8-byte alignment
	if entry.Size%8 != 0 {
		return fmt.Errorf("entry size %d not 8-byte aligned at offset %d (entry index %d)", entry.Size, offset, entryIndex)
	}

	// Validate that the entry pointer is 8-byte aligned
	entryPtr := uintptr(unsafe.Pointer(entry))
	if entryPtr%8 != 0 {
		return fmt.Errorf("entry pointer 0x%x not 8-byte aligned at offset %d", entryPtr, offset)
	}

	// If memory layout debugging is enabled, log layout information
	if IsDebugEnabled("memorylayout") {
		pathFieldOffset := uintptr(unsafe.Pointer(&entry.Path[0])) - entryPtr
		fmt.Fprintf(os.Stderr, "Entry %d: size=%d, ptr=0x%x, path_offset=%d\n",
			offset/int(minSize), entry.Size, entryPtr, pathFieldOffset)
	}

	return nil
}
