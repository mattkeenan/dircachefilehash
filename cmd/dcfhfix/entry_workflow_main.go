package main

import (
	"crypto/sha1"
	"fmt"
	"os"
	"unsafe"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// processEntriesWithWorkflow implements the complete safe workflow
// Returns (entriesFixed, entriesDiscarded, error)
func processEntriesWithWorkflow(indexFile string, pathSet map[string]bool, field, value string, options *ParsedOptions) (int, int, error) {
	// Load raw index data for safe processing
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read index file: %v", err)
	}

	if len(data) < dcfh.HeaderSize {
		return 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	// Create temporary index file for validated/fixed entries
	tmpIndexFile := indexFile + ".fix.tmp"
	defer func() {
		if _, err := os.Stat(tmpIndexFile); err == nil {
			os.Remove(tmpIndexFile)
		}
	}()

	// Create temp file with proper header
	err = createTempIndexWithHeader(data, tmpIndexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to create temp index: %v", err)
	}

	var entriesFixed, entriesDiscarded int

	// Process all entries using the safe workflow
	err = processAllEntriesWorkflow(data, pathSet, field, value, tmpIndexFile, &entriesFixed, &entriesDiscarded, options)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to process entries: %v", err)
	}

	// Finalize the temp index with proper checksum
	err = finalizeTempIndex(tmpIndexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to finalize temp index: %v", err)
	}

	// Atomically replace the original index file
	err = os.Rename(tmpIndexFile, indexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to replace original index file: %v", err)
	}

	return entriesFixed, entriesDiscarded, nil
}

// createTempIndexWithHeader creates a temp index file with the proper header
func createTempIndexWithHeader(originalData []byte, tmpIndexFile string) error {
	file, err := os.Create(tmpIndexFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer file.Close()

	// Copy the header from the original file
	// TODO: This should update entry count as we process entries
	_, err = file.Write(originalData[:dcfh.HeaderSize])
	if err != nil {
		return fmt.Errorf("failed to write header: %v", err)
	}

	return nil
}

// finalizeTempIndex calculates checksum and finalizes the temp index using pkg functions
func finalizeTempIndex(tmpIndexFile string) error {
	// Create DirectoryCache instance to access pkg checksum functions
	dc := dcfh.NewDirectoryCache("", "")
	defer dc.Close()

	// Open file for reading and writing
	file, err := os.OpenFile(tmpIndexFile, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to open temp index file: %v", err)
	}
	defer file.Close()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp index file: %v", err)
	}
	fileSize := stat.Size()

	if fileSize < dcfh.HeaderSize {
		return fmt.Errorf("temp index file too small: %d bytes", fileSize)
	}

	// Read the entire file
	data := make([]byte, fileSize)
	if _, err := file.ReadAt(data, 0); err != nil {
		return fmt.Errorf("failed to read temp index file: %v", err)
	}

	// Get header
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Count actual entries by parsing the file
	entryData := data[dcfh.HeaderSize:]
	var actualEntryCount uint32
	offset := 0

	for offset < len(entryData) {
		if offset+int(unsafe.Sizeof(binaryEntry{})) > len(entryData) {
			break
		}
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
		if entry.Size == 0 || int(entry.Size) > len(entryData)-offset {
			break
		}
		actualEntryCount++
		offset += int(entry.Size)
	}

	// Update entry count in header
	header.EntryCount = actualEntryCount

	// Set clean flag (following pkg pattern)
	header.Flags |= dcfh.IndexFlagClean

	// Calculate checksum following exact pkg pattern:
	// Hash header up to checksum field + hash all entry data
	hasher := sha1.New()

	// Hash header up to checksum field (following pkg implementation exactly)
	headerBytes := (*[dcfh.HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	// Hash entry data if any (following pkg implementation exactly)
	if len(entryData) > 0 {
		hasher.Write(entryData)
	}

	// Store checksum in header (following pkg implementation exactly)
	checksumBytes := hasher.Sum(nil)
	copy(header.Checksum[:], checksumBytes)

	// Write the updated header back to file
	if _, err := file.WriteAt(data[:dcfh.HeaderSize], 0); err != nil {
		return fmt.Errorf("failed to write updated header: %v", err)
	}

	// Sync to ensure data is written
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp index file: %v", err)
	}

	return nil
}
