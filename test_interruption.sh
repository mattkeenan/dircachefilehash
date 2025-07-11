#!/bin/bash
# Test script to verify partial scan results are saved to cache on interruption

set -e

# Create a test directory with many files
TEST_DIR="/tmp/dcfh_interrupt_test_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test repository with 1000 files..."
for i in $(seq 1 1000); do
    echo "content $i" > "file_$i.txt"
done

# Initialize dcfh
echo "Initializing dcfh..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" init .

# Start a status command in background and interrupt it quickly
echo "Running status command and interrupting it..."
timeout -s INT 0.1 "$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" status || true

# Check if cache file was created
if [ -f ".dcfh/cache.idx" ]; then
    CACHE_SIZE=$(stat -c%s ".dcfh/cache.idx" 2>/dev/null || stat -f%z ".dcfh/cache.idx" 2>/dev/null || echo "0")
    echo "SUCCESS: Cache file created with size: $CACHE_SIZE bytes"
    
    # Run status again to see if it uses the cache
    echo "Running status again to verify cache is used..."
    "$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan status 2>&1 | grep -E "WORKFLOW|entries|cache" | head -10
else
    echo "FAILED: No cache file created after interruption"
fi

# Cleanup
cd /
rm -rf "$TEST_DIR"