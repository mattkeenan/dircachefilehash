package dircachefilehash

import (
	"fmt"
	"os"
)

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

// getDefaultHashAlgorithm gets the default hash algorithm from config
func (dc *DirectoryCache) getDefaultHashAlgorithm() (*HashAlgorithm, error) {
	if dc.config == nil {
		// Fallback to SHA256 if no config
		return GetHashAlgorithm("sha256")
	}

	hashConfig := dc.config.GetHashConfig()
	return GetHashAlgorithm(hashConfig.Default)
}
