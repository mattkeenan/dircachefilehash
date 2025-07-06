package main

import (
	"encoding/json"
	"fmt"
	"os"
	"unsafe"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// EntryJSON represents the JSON format for entry append operations
type EntryJSON struct {
	Path           string  `json:"path"`
	FlagIsDeleted  bool    `json:"flag_is_deleted"`
	FileSize       uint64  `json:"file_size"`
	Mode           uint32  `json:"mode"`
	UID            uint32  `json:"uid"`
	GID            uint32  `json:"gid"`
	Dev            uint32  `json:"dev"`
	Ino            *uint32 `json:"ino,omitempty"`
	MTime          string  `json:"mtime"`
	CTime          string  `json:"ctime"`
	Hash           string  `json:"hash"`
	HashType       uint16  `json:"hash_type"`
}

// parseEntryFromJSON parses JSON data into a ValidatedEntry
func parseEntryFromJSON(jsonData string) (*ValidatedEntry, error) {
	var entryJSON EntryJSON
	if err := json.Unmarshal([]byte(jsonData), &entryJSON); err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	
	// Validate required fields
	if entryJSON.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if entryJSON.Hash == "" {
		return nil, fmt.Errorf("hash is required")
	}
	
	// Parse times
	mtime, err := parseTimeValue(entryJSON.MTime)
	if err != nil {
		return nil, fmt.Errorf("invalid mtime: %v", err)
	}
	
	ctime, err := parseTimeValue(entryJSON.CTime)
	if err != nil {
		return nil, fmt.Errorf("invalid ctime: %v", err)
	}
	
	// Parse and validate hash
	hashBytes, err := parseHashValue(entryJSON.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid hash: %v", err)
	}
	
	// Create the binaryEntry
	entry := &binaryEntry{
		Size:       0, // Will be calculated later when writing
		CTimeWall:  ctime,
		MTimeWall:  mtime,
		Dev:        entryJSON.Dev,
		Ino:        0, // Default if not provided
		Mode:       entryJSON.Mode,
		UID:        entryJSON.UID,
		GID:        entryJSON.GID,
		FileSize:   entryJSON.FileSize,
		EntryFlags: 0,
		HashType:   entryJSON.HashType,
	}
	
	// Set Ino if provided
	if entryJSON.Ino != nil {
		entry.Ino = *entryJSON.Ino
	}
	
	// Set deleted flag
	if entryJSON.FlagIsDeleted {
		entry.EntryFlags |= dcfh.EntryFlagDeleted
	}
	
	// Set hash (copy into fixed-size array)
	copy(entry.Hash[:], hashBytes)
	
	return &ValidatedEntry{
		Entry: entry,
		Path:  entryJSON.Path,
	}, nil
}

// processEntriesWithAppend processes entries and appends a new entry
func processEntriesWithAppend(indexFile string, newEntry *ValidatedEntry, options *ParsedOptions) (int, int, error) {
	// Load raw index data for safe processing
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read index file: %v", err)
	}
	
	if len(data) < dcfh.HeaderSize {
		return 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	// Create temporary index file for validated/fixed entries
	tmpIndexFile := indexFile + ".append.tmp"
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
	
	var entriesAdded, entriesDiscarded int
	
	// Process all existing entries first
	err = processAllEntriesForAppend(data, tmpIndexFile, &entriesDiscarded, options)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to process existing entries: %v", err)
	}
	
	// Append the new entry
	err = appendValidatedEntryToTmpIndex(tmpIndexFile, newEntry)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to append new entry: %v", err)
	}
	entriesAdded = 1
	
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
	
	return entriesAdded, entriesDiscarded, nil
}

// processEntriesWithRemoval processes entries and removes matching paths
func processEntriesWithRemoval(indexFile string, pathSet map[string]bool, options *ParsedOptions) (int, int, error) {
	// Load raw index data for safe processing
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read index file: %v", err)
	}
	
	if len(data) < dcfh.HeaderSize {
		return 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	// Create temporary index file for remaining entries
	tmpIndexFile := indexFile + ".remove.tmp"
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
	
	var entriesRemoved, entriesDiscarded int
	
	// Process all entries and exclude those that match removal paths
	err = processAllEntriesForRemoval(data, pathSet, tmpIndexFile, &entriesRemoved, &entriesDiscarded, options)
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
	
	return entriesRemoved, entriesDiscarded, nil
}

// processAllEntriesForAppend processes existing entries (for append operation)
func processAllEntriesForAppend(data []byte, tmpIndexFile string, entriesDiscarded *int, options *ParsedOptions) error {
	// Extract header information
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	entryCount := header.EntryCount
	entryData := data[dcfh.HeaderSize:]
	
	offset := 0
	unfixableEntryCount := 0
	unfixableEntryMax := 100
	
	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		// Try to get a validated entry from this offset
		validatedEntry, err := NewValidatedEntry(entryData, int(i), offset)
		if err != nil {
			// Entry is corrupted - discard with warning
			if !options.GetBool("quiet") {
				fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, err)
			}
			*entriesDiscarded++
			unfixableEntryCount++
			
			if unfixableEntryCount > unfixableEntryMax {
				return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableEntryCount)
			}
			
			// Try to skip to next entry
			if !trySkipToNextEntry(entryData, &offset) {
				break
			}
			continue
		}
		
		// Entry is valid - append it to temp index
		err = appendValidatedEntryToTmpIndex(tmpIndexFile, validatedEntry)
		if err != nil {
			return fmt.Errorf("failed to append valid entry %d: %v", i, err)
		}
		
		// Move to next entry
		offset += int(validatedEntry.Entry.Size)
	}
	
	return nil
}

// processAllEntriesForRemoval processes entries and excludes those matching removal paths
func processAllEntriesForRemoval(data []byte, pathSet map[string]bool, tmpIndexFile string, entriesRemoved, entriesDiscarded *int, options *ParsedOptions) error {
	// Extract header information
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	entryCount := header.EntryCount
	entryData := data[dcfh.HeaderSize:]
	
	offset := 0
	unfixableEntryCount := 0
	unfixableEntryMax := 100
	
	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		// Try to get a validated entry from this offset
		validatedEntry, err := NewValidatedEntry(entryData, int(i), offset)
		if err != nil {
			// Entry is corrupted - discard with warning
			if !options.GetBool("quiet") {
				fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, err)
			}
			*entriesDiscarded++
			unfixableEntryCount++
			
			if unfixableEntryCount > unfixableEntryMax {
				return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableEntryCount)
			}
			
			// Try to skip to next entry
			if !trySkipToNextEntry(entryData, &offset) {
				break
			}
			continue
		}
		
		// Check if this entry should be removed
		if pathSet[validatedEntry.Path] {
			// This entry matches removal criteria - skip it
			*entriesRemoved++
			if !options.GetBool("quiet") {
				fmt.Printf("Removing entry: %s\n", validatedEntry.Path)
			}
		} else {
			// Entry should be kept - append it to temp index
			err = appendValidatedEntryToTmpIndex(tmpIndexFile, validatedEntry)
			if err != nil {
				return fmt.Errorf("failed to append entry %d: %v", i, err)
			}
		}
		
		// Move to next entry
		offset += int(validatedEntry.Entry.Size)
	}
	
	return nil
}