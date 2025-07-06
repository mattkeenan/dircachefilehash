package dircachefilehash

import (
	"fmt"
	"os"
)


// hashFile calculates hash of a file's contents using the configured algorithm
func (dc *DirectoryCache) hashFile(filePath string) (string, error) {
	return dc.hashFileWithAlgorithm(filePath, nil)
}

// hashSymlinkTargetToBytes calculates hash of a symlink's target path and returns raw bytes
func (dc *DirectoryCache) hashSymlinkTargetToBytes(symlinkPath string) ([]byte, uint16, error) {
	// Get default hash algorithm from config
	algorithm, err := dc.getDefaultHashAlgorithm()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get default hash algorithm: %w", err)
	}
	
	// Read the symlink target path (not the target file contents)
	targetPath, err := os.Readlink(symlinkPath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read symlink target: %w", err)
	}
	
	// Hash the target path string
	hasher := algorithm.NewFunc()
	hasher.Write([]byte(targetPath))
	return hasher.Sum(nil), algorithm.TypeID, nil
}


// hashFileWithAlgorithm calculates hash of a file using the specified algorithm or default
func (dc *DirectoryCache) hashFileWithAlgorithm(filePath string, algorithm *HashAlgorithm) (string, error) {
	// Use provided algorithm or get default from config
	if algorithm == nil {
		var err error
		algorithm, err = dc.getDefaultHashAlgorithm()
		if err != nil {
			return "", fmt.Errorf("failed to get default hash algorithm: %w", err)
		}
	}
	
	return HashFileToHexString(filePath, algorithm)
}

// hashFileWithAlgorithmToBytes calculates hash and returns raw bytes with type info
func (dc *DirectoryCache) hashFileWithAlgorithmToBytes(filePath string, algorithm *HashAlgorithm) ([]byte, uint16, error) {
	// Use provided algorithm or get default from config
	if algorithm == nil {
		var err error
		algorithm, err = dc.getDefaultHashAlgorithm()
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get default hash algorithm: %w", err)
		}
	}
	
	hashBytes, err := HashFile(filePath, algorithm)
	if err != nil {
		return nil, 0, err
	}
	
	return hashBytes, algorithm.TypeID, nil
}

// getDefaultHashAlgorithm gets the default hash algorithm from config
func (dc *DirectoryCache) getDefaultHashAlgorithm() (*HashAlgorithm, error) {
	if dc.config == nil {
		// Fallback to SHA256 if no config
		return GetHashAlgorithm("sha256")
	}
	
	hashConfig := dc.config.GetHashConfig()
	return GetHashAlgorithm(hashConfig.Default)
}
