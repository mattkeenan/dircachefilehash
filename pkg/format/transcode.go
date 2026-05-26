package format

import (
	"fmt"
	"unsafe"
)

// TranscodeLegacyIndex reads a complete legacy (v2/v3) index image and returns a
// complete v4 index image: a v4 header followed by v4-layout entries (Dev/Ino
// widened to 64 bits, every later field shifted +8, Size recomputed). The legacy
// bytes are never cast as a v4 Entry — each entry is decoded at its legacy
// offsets and re-emitted, so reading an old index can never misread the tail.
//
// It is exported and usable standalone (not only behind the load path's
// checkEntryRegionAccess), so it self-validates and trusts nothing:
//   - the buffer is bounds-checked before the header cast and before each entry;
//   - the header EntryCount NEVER sizes an allocation — the output grows
//     incrementally per validated entry, so a crafted 0xFFFFFFFF count against a
//     tiny file errors instead of attempting a multi-GB allocation.
func TranscodeLegacyIndex(legacyData []byte) ([]byte, error) {
	if len(legacyData) < V2HeaderSize {
		return nil, fmt.Errorf("legacy index too small: %d bytes < %d", len(legacyData), V2HeaderSize)
	}

	// Reading Version/EntryCount/Flags/ChecksumType only touches the first 28
	// bytes of the header, well inside V2HeaderSize, so the cast is in bounds for
	// both v2 (88) and v3 (104) even though Header is 104 bytes.
	srcHdr := (*Header)(unsafe.Pointer(&legacyData[0]))
	version := srcHdr.Version
	if version < MinIndexVersion || version >= CurrentIndexVersion {
		return nil, fmt.Errorf("transcode: version %d is not a legacy version (supported %d-%d)",
			version, MinIndexVersion, CurrentIndexVersion-1)
	}

	hdrSize := headerSizeForVersion(version)
	if len(legacyData) < hdrSize {
		return nil, fmt.Errorf("transcode: too small for v%d header: %d < %d", version, len(legacyData), hdrSize)
	}

	lay, err := layoutForVersion(version)
	if err != nil {
		return nil, err
	}

	// Emit the v4 header first (header-only baseline; entries appended below).
	out := make([]byte, HeaderSize)
	outHdr := (*Header)(unsafe.Pointer(&out[0]))
	outHdr.SetHeaderForWritableIndex(srcHdr.Signature, srcHdr.EntryCount, srcHdr.Flags, srcHdr.ChecksumType)
	// Preserve the source write-time for v3; v2 has no Timestamp field → 0.
	if version >= TimestampMinVersion {
		outHdr.Timestamp = srcHdr.Timestamp
	} else {
		outHdr.Timestamp = 0
	}

	entryRegion := legacyData[hdrSize:]
	offset := 0
	for i := uint32(0); i < srcHdr.EntryCount; i++ {
		if offset+4 > len(entryRegion) {
			return nil, fmt.Errorf("transcode: cannot read size of entry %d at offset %d", i, offset)
		}
		size := *(*uint32)(unsafe.Pointer(&entryRegion[offset]))
		if size < uint32(lay.minSize) { //nolint:gosec // G115: struct size, bounded non-negative
			return nil, fmt.Errorf("transcode: entry %d size %d below legacy minimum %d", i, size, lay.minSize)
		}
		if size > 4096 {
			return nil, fmt.Errorf("transcode: entry %d size %d unreasonably large", i, size)
		}
		if offset+int(size) > len(entryRegion) {
			return nil, fmt.Errorf("transcode: entry %d size %d extends beyond region (offset %d, len %d)",
				i, size, offset, len(entryRegion))
		}

		v4Entry := transcodeEntry(entryRegion[offset:offset+int(size)], lay)
		out = append(out, v4Entry...)
		offset += int(size)
	}

	if offset != len(entryRegion) {
		return nil, fmt.Errorf("transcode: consumed %d of %d entry bytes (EntryCount %d)",
			offset, len(entryRegion), srcHdr.EntryCount)
	}

	return out, nil
}

// transcodeEntry converts one bounds-validated legacy entry (>= lay.minSize,
// fully inside its declared Size) into v4-layout bytes: fields copied across at
// the source layout's offsets (Dev/Ino widened from 32→64 bits), the
// variable-length path re-appended after the v4 struct, and Size recomputed for
// the v4 stride. Reading by offset (rather than a struct cast) keeps it agnostic
// to which legacy version produced the bytes. Only legacy layouts (32-bit
// Dev/Ino) reach here — TranscodeLegacyIndex rejects current/unknown versions.
func transcodeEntry(src []byte, lay entryLayout) []byte {
	// Path is the NUL-terminated run after the source fixed portion.
	pathBytes := src[lay.minSize:]
	pathLen := len(pathBytes)
	for i, b := range pathBytes {
		if b == 0 {
			pathLen = i
			break
		}
	}

	v4Size := BESizeFromPathLen(pathLen)
	out := make([]byte, v4Size)
	dst := (*Entry)(unsafe.Pointer(&out[0]))
	dst.Size = RecordSize(v4Size) //nolint:gosec // G115: v4Size = BESizeFromPathLen(pathLen); pathLen bounded by the validated legacy entry Size (≤ 4096)
	dst.CTimeWall = *(*WallTime)(unsafe.Pointer(&src[lay.cTimeWall]))
	dst.MTimeWall = *(*WallTime)(unsafe.Pointer(&src[lay.mTimeWall]))
	dst.Dev = DevID(*(*uint32)(unsafe.Pointer(&src[lay.dev]))) // uint32 → uint64 (the widen)
	dst.Ino = Inode(*(*uint32)(unsafe.Pointer(&src[lay.ino]))) // uint32 → uint64 (the widen)
	dst.Mode = *(*FileMode)(unsafe.Pointer(&src[lay.mode]))
	dst.UID = *(*UserID)(unsafe.Pointer(&src[lay.uid]))
	dst.GID = *(*GroupID)(unsafe.Pointer(&src[lay.gid]))
	dst.FileSize = *(*ByteSize)(unsafe.Pointer(&src[lay.fileSize]))
	dst.EntryFlags = *(*FlagBits)(unsafe.Pointer(&src[lay.entryFlags]))
	dst.HashType = *(*HashKind)(unsafe.Pointer(&src[lay.hashType]))
	copy(dst.Hash[:], src[lay.hash:lay.hash+64])
	copy(out[unsafe.Sizeof(Entry{}):], pathBytes[:pathLen])
	return out
}
