#!/bin/bash
# Test that actually interrupts the scan process

set -e

TEST_DIR="/tmp/dcfh_real_interrupt_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test repository with 10000 files..."
for dir in $(seq 1 10); do
    mkdir -p "dir_$dir"
    for i in $(seq 1 1000); do
        echo "content $dir $i" > "dir_$dir/file_$i.txt"
    done
done

# Initialize dcfh
echo "Initializing dcfh..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" init .

# Run status in background and interrupt it almost immediately
echo "Running status command with immediate interrupt..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan,scanning status 2>&1 &
PID=$!

# Give it just enough time to start but not finish
sleep 0.05
echo -e "\n=== SENDING INTERRUPT SIGNAL ==="
kill -INT $PID

# Wait and capture exit code
wait $PID 2>/dev/null
EXIT_CODE=$?
echo "Exit code: $EXIT_CODE"

# Look for evidence of interruption
echo -e "\n=== Checking for interruption evidence ==="
if [ -f ".dcfh/cache.idx" ]; then
    CACHE_SIZE=$(stat -c%s ".dcfh/cache.idx" 2>/dev/null || stat -f%z ".dcfh/cache.idx" 2>/dev/null || echo "0")
    echo "Cache file exists with size: $CACHE_SIZE bytes"
    
    # Check for scan index files (these would exist if scan was interrupted)
    if ls .dcfh/scan-*.idx 2>/dev/null; then
        echo "Found scan index files (scan was in progress when interrupted)"
    else
        echo "No scan index files found"
    fi
else
    echo "No cache file found"
fi

ls -la .dcfh/

# Cleanup
cd /
rm -rf "$TEST_DIR"