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
	Name     string
	TypeID   uint16
	Size     int
	NewFunc  func() hash.Hash
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