package main

import (
	"fmt"
	"os"
	"unsafe"
	
	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <input-v1.idx> <output-v2.idx>\n", os.Args[0])
		os.Exit(1)
	}
	
	inputFile := os.Args[1]
	outputFile := os.Args[2]
	
	fmt.Printf("Converting %s to v2 format...\n", inputFile)
	
	// Process the index file
	
	// Use IterateIndexFile to process entries
	convertedCount := 0
	totalCount := 0
	
	// First pass: count and identify entries that need hashing flag
	entries := make([]*dircachefilehash.EntryInfo, 0)
	
	err := dircachefilehash.IterateIndexFile(inputFile, func(entry *dircachefilehash.EntryInfo, indexType string) bool {
		totalCount++
		entries = append(entries, entry)
		
		// Check if entry has a valid hash
		hasValidHash := false
		if entry.HashType != 0 && entry.HashStr != "" {
			// Check if hash string is not all zeros
			allZeros := true
			for _, char := range entry.HashStr {
				if char != '0' {
					allZeros = false
					break
				}
			}
			hasValidHash = !allZeros
		}
		
		if hasValidHash {
			convertedCount++
		}
		
		return true // Continue iteration
	})
	
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing index: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Processed %d entries, %d will be marked as hashed\n", totalCount, convertedCount)
	
	// Read the file data for binary manipulation
	data, err := os.ReadFile(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}
	
	// Use the EXACT structs from the codebase
	// indexHeader from pkg/index.go:23-31
	type indexHeader struct {
		Signature    [4]byte  // "dcfh" signature
		ByteOrder    uint64   // Byte order detection magic (0x0102030405060708) - MUST be checked before other fields
		Version      uint32   // Index version (host order)
		EntryCount   uint32   // Number of entries (host order)
		Flags        uint16   // Index flags (host order) - matches binaryEntry.EntryFlags size
		ChecksumType uint16   // Checksum algorithm type (matches binaryEntry.HashType size)
		Checksum     [64]byte // Checksum of header+entries (up to 512-bit support)
	}
	
	// Get the correct offsets using unsafe.Offsetof
	byteOrderOffset := unsafe.Offsetof(indexHeader{}.ByteOrder)
	versionOffset := unsafe.Offsetof(indexHeader{}.Version)
	headerSize := unsafe.Sizeof(indexHeader{})
	
	fmt.Printf("Header offsets: ByteOrder=%d, Version=%d, HeaderSize=%d\n", byteOrderOffset, versionOffset, headerSize)
	
	if len(data) >= int(headerSize) {
		// Check if this looks like a dcfh file
		if string(data[0:4]) == "dcfh" {
			// Check byte order magic
			byteOrderBytes := data[byteOrderOffset : byteOrderOffset+8]
			expectedMagic := []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01} // Little endian
			
			isLittleEndian := true
			for i := 0; i < 8; i++ {
				if byteOrderBytes[i] != expectedMagic[i] {
					isLittleEndian = false
					break
				}
			}
			
			var currentVersion uint32
			if isLittleEndian {
				// Read version (little endian)
				currentVersion = uint32(data[versionOffset]) | uint32(data[versionOffset+1])<<8 | 
					uint32(data[versionOffset+2])<<16 | uint32(data[versionOffset+3])<<24
			} else {
				// Read version (big endian)
				currentVersion = uint32(data[versionOffset+3]) | uint32(data[versionOffset+2])<<8 | 
					uint32(data[versionOffset+1])<<16 | uint32(data[versionOffset])<<24
			}
			
			fmt.Printf("Detected %s endian format, current version: %d\n", 
				map[bool]string{true: "little", false: "big"}[isLittleEndian], currentVersion)
			
			if currentVersion == 1 {
				// Update to version 2 using same endianness
				if isLittleEndian {
					data[versionOffset] = 2
					data[versionOffset+1] = 0
					data[versionOffset+2] = 0
					data[versionOffset+3] = 0
				} else {
					data[versionOffset+3] = 2
					data[versionOffset+2] = 0
					data[versionOffset+1] = 0
					data[versionOffset] = 0
				}
				fmt.Printf("Updated version from %d to 2\n", currentVersion)
				
				// Now update the EntryFlagHashed bits for entries with valid hashes
				// Use the actual HeaderSize constant (88) not our struct size (96)
				updatedEntries := updateEntryHashedFlags(data, 88, entries)
				fmt.Printf("Updated %d entries with hashed flags\n", updatedEntries)
			} else {
				fmt.Printf("Input file is already version %d\n", currentVersion)
			}
		}
	}
	
	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Printf("Conversion complete: %s\n", outputFile)
}

func getExpectedHashLength(hashType uint16) int {
	switch hashType {
	case 1: // SHA1
		return 20
	case 2: // SHA256  
		return 32
	case 3: // SHA512
		return 64
	default:
		return 0
	}
}

func updateEntryHashedFlags(data []byte, headerSize int, entries []*dircachefilehash.EntryInfo) int {
	// Use the EXACT binaryEntry struct from pkg/util.go:67-81
	type binaryEntry struct {
		Size       uint32   // Total size of this entry including padding (host order) - MUST BE FIRST
		CTimeWall  uint64   // Change time wall clock (Go wall time format)
		MTimeWall  uint64   // Modification time wall clock (Go wall time format)
		Dev        uint32   // Device ID (host order)
		Ino        uint32   // Inode number (host order)
		Mode       uint32   // File mode (host order)
		UID        uint32   // User ID (host order)
		GID        uint32   // Group ID (host order)
		FileSize   uint64   // File size in bytes (host order) - supports files >4GB
		EntryFlags uint16   // Entry Flags
		HashType   uint16   // Hash algorithm type (SHA1=1, SHA256=2, SHA512=3)
		Hash       [64]byte // Hash value (up to 64 bytes for SHA-512)
		Path       [8]byte  // Path as bytes, actual length variable but must be at least 8 bytes long
	}
	
	// Use unsafe.Offsetof for ALL the fields I need
	sizeOffset := unsafe.Offsetof(binaryEntry{}.Size)         // Should be 0
	entryFlagsOffset := unsafe.Offsetof(binaryEntry{}.EntryFlags)
	hashTypeOffset := unsafe.Offsetof(binaryEntry{}.HashType)
	hashOffset := unsafe.Offsetof(binaryEntry{}.Hash)
	
	fmt.Printf("binaryEntry offsets: Size=%d, EntryFlags=%d, HashType=%d, Hash=%d, TotalSize=%d\n", 
		sizeOffset, entryFlagsOffset, hashTypeOffset, hashOffset, unsafe.Sizeof(binaryEntry{}))
	
	const EntryFlagHashed uint16 = 1 << 1 // From constants.go
	
	updatedCount := 0
	offset := headerSize
	entryIndex := 0
	
	for offset < len(data) && entryIndex < len(entries) {
		if offset+4 > len(data) {
			break
		}
		
		// Read entry size using the proper offset
		sizePos := offset + int(sizeOffset)
		entrySize := uint32(data[sizePos]) | uint32(data[sizePos+1])<<8 | 
			uint32(data[sizePos+2])<<16 | uint32(data[sizePos+3])<<24
			
		if entryIndex < 5 { // Debug first few entries
			fmt.Printf("Entry %d at offset %d: size bytes = %02x %02x %02x %02x = %d\n", 
				entryIndex, sizePos, data[sizePos], data[sizePos+1], data[sizePos+2], data[sizePos+3], entrySize)
		}
			
		if entrySize < 48 || entrySize > 4096 || offset+int(entrySize) > len(data) {
			fmt.Printf("Warning: invalid entry size %d at offset %d\n", entrySize, offset)
			break
		}
		
		// Get the corresponding EntryInfo
		entry := entries[entryIndex]
		
		// Check if this entry should have hashed flag set
		shouldBeHashed := false
		if entry.HashType != 0 && entry.HashStr != "" {
			// Check if hash string is not all zeros
			allZeros := true
			for _, char := range entry.HashStr {
				if char != '0' {
					allZeros = false
					break
				}
			}
			shouldBeHashed = !allZeros
		}
		
		if shouldBeHashed {
			// Set the EntryFlagHashed bit
			flagsPos := offset + int(entryFlagsOffset)
			if flagsPos+2 <= len(data) {
				currentFlags := uint16(data[flagsPos]) | uint16(data[flagsPos+1])<<8
				newFlags := currentFlags | EntryFlagHashed
				data[flagsPos] = byte(newFlags)
				data[flagsPos+1] = byte(newFlags >> 8)
				updatedCount++
			}
		}
		
		offset += int(entrySize)
		entryIndex++
	}
	
	return updatedCount
}