package dircachefilehash

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
	"time"
)

// HashAlgorithm represents a hash algorithm configuration
type HashAlgorithm struct {
	Name    string
	TypeID  uint16
	Size    int
	NewFunc func() hash.Hash
}

// GetHashAlgorithm returns the hash algorithm configuration for the given name
func GetHashAlgorithm(name string) (*HashAlgorithm, error) {
	switch strings.ToLower(name) {
	case "sha1":
		return &HashAlgorithm{
			Name:    "sha1",
			TypeID:  HashTypeSHA1,
			Size:    HashSizeSHA1,
			NewFunc: sha1.New,
		}, nil
	case "sha256":
		return &HashAlgorithm{
			Name:    "sha256",
			TypeID:  HashTypeSHA256,
			Size:    HashSizeSHA256,
			NewFunc: sha256.New,
		}, nil
	case "sha512":
		return &HashAlgorithm{
			Name:    "sha512",
			TypeID:  HashTypeSHA512,
			Size:    HashSizeSHA512,
			NewFunc: sha512.New,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported hash algorithm: %s", name)
	}
}

// GetHashAlgorithmByType returns the hash algorithm configuration for the given type ID
func GetHashAlgorithmByType(typeID uint16) (*HashAlgorithm, error) {
	switch typeID {
	case HashTypeSHA1:
		return GetHashAlgorithm("sha1")
	case HashTypeSHA256:
		return GetHashAlgorithm("sha256")
	case HashTypeSHA512:
		return GetHashAlgorithm("sha512")
	default:
		return nil, fmt.Errorf("unsupported hash type ID: %d", typeID)
	}
}

// HashFile calculates the hash of a file using the specified algorithm
func HashFile(filePath string, algorithm *HashAlgorithm) ([]byte, error) {
	// Start timing if debug=hash and verbose >= 3
	var startTime time.Time
	var fileSize int64

	if IsDebugEnabled("hash") && GetVerboseLevel() >= 3 {
		startTime = time.Now()
		// Get file size for rate calculation
		if info, err := os.Stat(filePath); err == nil {
			fileSize = info.Size()
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := algorithm.NewFunc()
	written, err := io.Copy(hasher, file)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	result := hasher.Sum(nil)

	// Log timing information if enabled
	if IsDebugEnabled("hash") && GetVerboseLevel() >= 3 && !startTime.IsZero() {
		duration := time.Since(startTime)
		if duration > 0 {
			rate := float64(written) / duration.Seconds()
			fmt.Fprintf(os.Stderr, "[HASH] %s: %s hashed in %v (%s)\n",
				filePath,
				FormatHumanSize(fileSize),
				duration,
				FormatHumanRate(rate))
		}
	}

	return result, nil
}

// HashFileToHexString calculates the hash of a file and returns it as a hex string
func HashFileToHexString(filePath string, algorithm *HashAlgorithm) (string, error) {
	hashBytes, err := HashFile(filePath, algorithm)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hashBytes), nil
}

// HashStringToHexString calculates the hash of a string and returns it as a hex string
func HashStringToHexString(data string, algorithm *HashAlgorithm) (string, error) {
	hasher := algorithm.NewFunc()
	hasher.Write([]byte(data))
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// GetDefaultHashSize returns the size for a hash type (for backwards compatibility)
func GetHashSize(hashType uint16) int {
	switch hashType {
	case HashTypeSHA1:
		return HashSizeSHA1
	case HashTypeSHA256:
		return HashSizeSHA256
	case HashTypeSHA512:
		return HashSizeSHA512
	default:
		return HashSizeSHA1 // fallback
	}
}

// GetCurrentHashType returns the current hash type to use based on command line options,
// config file settings, and defaults (in that order of precedence)
func (dc *DirectoryCache) GetCurrentHashType() uint16 {
	// 1. Check command line options first (via overrides)
	// Command line overrides are already applied to the config during initialization

	// 2. Check config file settings (which may include command line overrides)
	if dc.config != nil {
		hashConfig := dc.config.GetHashConfig()
		if hashConfig != nil && hashConfig.Default != "" {
			// Get the hash algorithm configuration
			if algorithm, err := GetHashAlgorithm(hashConfig.Default); err == nil {
				return algorithm.TypeID
			}
		}
	}

	// 3. Default to SHA256 (as specified in requirements)
	return HashTypeSHA256
}

// GetCurrentHashAlgorithm returns the current hash algorithm configuration
func (dc *DirectoryCache) GetCurrentHashAlgorithm() (*HashAlgorithm, error) {
	hashType := dc.GetCurrentHashType()
	return GetHashAlgorithmByType(hashType)
}

// HashFileInterruptible calculates the hash of a file using a caller-supplied
// buffer and checks for shutdown signals between buffer reads for graceful
// interruption. Passing a pre-allocated buffer avoids per-file heap allocation,
// which dramatically reduces GC pressure when hashing many files.
func HashFileInterruptible(ctx context.Context, filePath string, algorithm *HashAlgorithm, buffer []byte) ([]byte, error) {
	// Start timing if debug=hash and verbose >= 3
	var startTime time.Time
	var fileSize int64

	if IsDebugEnabled("hash") && GetVerboseLevel() >= 3 {
		startTime = time.Now()
		// Get file size for rate calculation
		if info, err := os.Stat(filePath); err == nil {
			fileSize = info.Size()
		}
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := algorithm.NewFunc()
	var totalRead int64

	for {
		// Check for shutdown signal before each read
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("hash operation interrupted: %w", ctx.Err())
		default:
			// Continue with read
		}

		n, err := file.Read(buffer)
		if n > 0 {
			hasher.Write(buffer[:n])
			totalRead += int64(n)
		}

		if err == io.EOF {
			// Successfully reached end of file
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read from file %s: %w", filePath, err)
		}
	}

	result := hasher.Sum(nil)

	// Log timing information if enabled
	if IsDebugEnabled("hash") && GetVerboseLevel() >= 3 && !startTime.IsZero() {
		duration := time.Since(startTime)
		if duration > 0 {
			rate := float64(totalRead) / duration.Seconds()
			fmt.Fprintf(os.Stderr, "[HASH] %s: %s hashed in %v (%s)\n",
				filePath,
				FormatHumanSize(fileSize),
				duration,
				FormatHumanRate(rate))
		}
	}

	return result, nil
}

// HashFileInterruptibleToBytes is a convenience function that also returns the
// type ID. If buffer is nil, a new buffer is allocated from the configured size.
// Callers that hash many files (e.g. worker pools) should pre-allocate a buffer
// and pass it here to avoid per-file allocation and GC pressure.
func (dc *DirectoryCache) HashFileInterruptibleToBytes(ctx context.Context, filePath string, buffer []byte) ([]byte, uint16, error) {
	// Get default algorithm
	algorithm, err := dc.getDefaultHashAlgorithm()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get default hash algorithm: %w", err)
	}

	// Allocate buffer if caller didn't provide one
	if buffer == nil {
		bufferSize, err := dc.getHashBufferSize()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get hash buffer size: %w", err)
		}
		buffer = make([]byte, bufferSize)
	}

	hashBytes, err := HashFileInterruptible(ctx, filePath, algorithm, buffer)
	if err != nil {
		return nil, 0, err
	}

	return hashBytes, algorithm.TypeID, nil
}

// getHashBufferSize gets the configured hash buffer size in bytes
func (dc *DirectoryCache) getHashBufferSize() (int, error) {
	if dc.config == nil {
		// Fallback to 2MB if no config
		return 2 * 1024 * 1024, nil
	}

	performanceConfig := dc.config.GetPerformanceConfig()
	return ParseHumanSize(performanceConfig.HashBuffer)
}

// hashSymlinkTargetToBytes calculates hash of a symlink's target path and returns raw bytes
func (dc *DirectoryCache) hashSymlinkTargetToBytes(symlinkPath string) ([]byte, uint16, error) {
	algorithm, err := dc.getDefaultHashAlgorithm()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get default hash algorithm: %w", err)
	}

	targetPath, err := os.Readlink(symlinkPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read symlink target: %w", err)
	}

	hasher := algorithm.NewFunc()
	hasher.Write([]byte(targetPath))
	return hasher.Sum(nil), algorithm.TypeID, nil
}

// getDefaultHashAlgorithm gets the default hash algorithm from config, falling
// back to SHA256 when no config is loaded.
func (dc *DirectoryCache) getDefaultHashAlgorithm() (*HashAlgorithm, error) {
	if dc.config == nil {
		return GetHashAlgorithm("sha256")
	}

	hashConfig := dc.config.GetHashConfig()
	return GetHashAlgorithm(hashConfig.Default)
}
