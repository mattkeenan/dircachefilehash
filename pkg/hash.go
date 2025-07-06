package dircachefilehash

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
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
			NewFunc: func() hash.Hash { return sha1.New() },
		}, nil
	case "sha256":
		return &HashAlgorithm{
			Name:    "sha256",
			TypeID:  HashTypeSHA256,
			Size:    HashSizeSHA256,
			NewFunc: func() hash.Hash { return sha256.New() },
		}, nil
	case "sha512":
		return &HashAlgorithm{
			Name:    "sha512",
			TypeID:  HashTypeSHA512,
			Size:    HashSizeSHA512,
			NewFunc: func() hash.Hash { return sha512.New() },
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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := algorithm.NewFunc()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	return hasher.Sum(nil), nil
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

// HashFileInterruptible calculates the hash of a file using a configurable buffer size
// and checks for shutdown signals between buffer reads for graceful interruption
func HashFileInterruptible(filePath string, algorithm *HashAlgorithm, bufferSize int, shutdownChan <-chan struct{}) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := algorithm.NewFunc()
	buffer := make([]byte, bufferSize)

	for {
		// Check for shutdown signal before each read
		select {
		case <-shutdownChan:
			return nil, fmt.Errorf("hash operation interrupted by shutdown")
		default:
			// Continue with read
		}

		n, err := file.Read(buffer)
		if n > 0 {
			hasher.Write(buffer[:n])
		}

		if err == io.EOF {
			// Successfully reached end of file
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read from file %s: %w", filePath, err)
		}
	}

	return hasher.Sum(nil), nil
}

// HashFileInterruptibleToBytes is a convenience function that also returns the type ID
func (dc *DirectoryCache) HashFileInterruptibleToBytes(filePath string, shutdownChan <-chan struct{}) ([]byte, uint16, error) {
	// Get default algorithm
	algorithm, err := dc.getDefaultHashAlgorithm()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get default hash algorithm: %w", err)
	}

	// Get buffer size from config
	bufferSize, err := dc.getHashBufferSize()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get hash buffer size: %w", err)
	}

	hashBytes, err := HashFileInterruptible(filePath, algorithm, bufferSize, shutdownChan)
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
