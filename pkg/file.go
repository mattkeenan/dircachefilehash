package dircachefilehash

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// timeWall extracts the wall field from time.Time using unsafe operations
func timeWall(t time.Time) uint64 {
	return *(*uint64)(unsafe.Pointer(&t))
}

// timeFromWall reconstructs a time.Time from wall time format
func timeFromWall(wall uint64) time.Time {
	var t time.Time
	*(*uint64)(unsafe.Pointer(&t)) = wall
	return t
}

// encodeWallTime directly encodes seconds and nanoseconds into Go's wall time format
func encodeWallTime(sec int64, nsec int64) uint64 {
	// Create time.Time with full nanosecond precision and extract wall time
	t := time.Unix(sec, nsec)
	return timeWall(t)
}

// writeEntryToMmap writes a binaryEntry directly to mmap'd memory
func (dc *DirectoryCache) writeEntryToMmap(data []byte, relPath string, hash [20]byte, info os.FileInfo, stat *syscall.Stat_t) int {
	// Write binaryEntry directly to mmap'd memory
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))

	// Use encodeWallTime() for both timestamps with raw syscall data
	entry.CTimeWall = encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	entry.MTimeWall = encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	entry.Dev = uint32(stat.Dev)
	entry.Ino = uint32(stat.Ino)
	entry.Mode = uint32(info.Mode())
	entry.UID = stat.Uid
	entry.GID = stat.Gid
	entry.Size = uint32(info.Size())
	entry.Hash = hash
	entry.Flags = uint16(len(relPath))
	entry.PathLen = uint16(len(relPath))

	// Write variable-size path directly after struct
	pathOffset := int(unsafe.Sizeof(*entry))
	copy(data[pathOffset:pathOffset+len(relPath)], relPath)

	// Add null terminator
	data[pathOffset+len(relPath)] = 0

	// Calculate total size with padding
	totalSize := pathOffset + len(relPath) + 1
	padding := (8 - (totalSize % 8)) % 8

	// Zero out padding
	for i := 0; i < padding; i++ {
		data[totalSize+i] = 0
	}

	return totalSize + padding
}

// processFileJob processes a single file job and returns hash and file info
func (dc *DirectoryCache) processFileJob(job fileJob) ([20]byte, *syscall.Stat_t, error) {
	// Hash the file contents
	hashStr, err := dc.hashFile(job.path)
	if err != nil {
		return [20]byte{}, nil, fmt.Errorf("failed to hash file %s: %w", job.path, err)
	}

	// Convert hash string to bytes
	hashBytes, err := hex.DecodeString(hashStr)
	if err != nil {
		return [20]byte{}, nil, fmt.Errorf("invalid hash %s: %w", hashStr, err)
	}

	var hash [20]byte
	copy(hash[:], hashBytes)

	// Get system-specific file information
	stat := job.info.Sys().(*syscall.Stat_t)

	return hash, stat, nil
}

// hashFile calculates SHA-1 hash of a file's contents
func (dc *DirectoryCache) hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}
