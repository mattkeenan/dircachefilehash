#!/bin/bash
# Test script to verify partial scan results are saved to cache on interruption

set -e

# Create a test directory with MANY files to ensure interruption happens
TEST_DIR="/tmp/dcfh_interrupt_test3_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test repository with 50000 files in nested directories..."
for dir in $(seq 1 50); do
    mkdir -p "dir_$dir"
    for i in $(seq 1 1000); do
        echo "content $dir $i" > "dir_$dir/file_$i.txt"
    done
done

# Initialize dcfh
echo "Initializing dcfh..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" init .

# Start a status command in background and interrupt it after a very short time
echo "Running status command with debug enabled..."
echo "Will interrupt after 0.2 seconds..."

# Run in background and capture both stdout and stderr
("$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan,scanning status 2>&1) &
PID=$!

# Sleep briefly then send interrupt
sleep 0.2
kill -INT $PID
wait $PID 2>/dev/null || true

echo -e "\n--- Checking results ---"

# Check if cache file was created
if [ -f ".dcfh/cache.idx" ]; then
    CACHE_SIZE=$(stat -c%s ".dcfh/cache.idx" 2>/dev/null || stat -f%z ".dcfh/cache.idx" 2>/dev/null || echo "0")
    echo "Cache file found with size: $CACHE_SIZE bytes"
    
    # Look for scan interruption messages in debug output
    echo -e "\nChecking for interruption evidence..."
    
    # Run status again to see what happens
    echo -e "\nRunning status again to check cache usage..."
    "$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan status 2>&1 | grep -E "(interrupt|WORKFLOW|Scan error|entries|cache)" | head -10
    
    ls -la .dcfh/
else
    echo "FAILED: No cache file created after interruption"
    ls -la .dcfh/
fi

# Cleanup
cd /
rm -rf "$TEST_DIR"