#!/bin/bash
# Test script to verify partial scan results are saved to cache on interruption

set -e

# Create a test directory with MANY files to ensure interruption happens
TEST_DIR="/tmp/dcfh_interrupt_test2_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test repository with 10000 files in nested directories..."
for dir in $(seq 1 10); do
    mkdir -p "dir_$dir"
    for i in $(seq 1 1000); do
        echo "content $dir $i" > "dir_$dir/file_$i.txt"
    done
done

# Initialize dcfh
echo "Initializing dcfh..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" init .

# Start a status command in background and interrupt it after 0.5 seconds
echo "Running status command with debug enabled..."
echo "Will interrupt after 0.5 seconds..."
timeout -s INT 0.5 "$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan,scanning status 2>&1 | tail -20

echo -e "\n--- Checking results ---"

# Check if cache file was created
if [ -f ".dcfh/cache.idx" ]; then
    CACHE_SIZE=$(stat -c%s ".dcfh/cache.idx" 2>/dev/null || stat -f%z ".dcfh/cache.idx" 2>/dev/null || echo "0")
    echo "SUCCESS: Cache file created with size: $CACHE_SIZE bytes"
    
    # Show cache entries count
    echo "Checking cache contents..."
    ls -la .dcfh/
else
    echo "FAILED: No cache file created after interruption"
    ls -la .dcfh/
fi

# Cleanup
cd /
rm -rf "$TEST_DIR"