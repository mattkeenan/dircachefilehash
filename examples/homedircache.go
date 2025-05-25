package main

import (
	"fmt"
	"log"
	"os"

	"github.com/mattkeenan/dircachefilehash"
)

func main() {
	// Get user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}

	// Create a new directory cache
	cache := dircachefilehash.NewDirectoryCache(homeDir, ".dircachefilehash/index")

	// Scan directory and create index
	if err := cache.Update(); err != nil {
		log.Fatal(err)
	}

	// Get statistics
	fileCount, totalSize, _ := cache.Stats()
	fmt.Printf("Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
}
