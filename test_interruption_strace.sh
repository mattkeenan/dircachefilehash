#!/bin/bash
# Test script using strace to verify partial scan results are saved to cache on interruption

set -e

# Create a test directory with MANY files to ensure interruption happens
TEST_DIR="/tmp/dcfh_interrupt_strace_$$"
STRACE_FILE="/tmp/dcfh_strace_$$.log"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test repository with 100000 files in nested directories..."
for dir in $(seq 1 100); do
    mkdir -p "dir_$dir"
    for i in $(seq 1 1000); do
        echo "content $dir $i" > "dir_$dir/file_$i.txt"
    done
done

# Initialize dcfh
echo "Initializing dcfh..."
"$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" init .

# Run status under strace and interrupt it
echo "Running status command under strace..."
echo "Will interrupt after 1 second..."

# Run strace in background
strace -f -o "$STRACE_FILE" "$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)/dcfh" --debug=scan status 2>&1 &
STRACE_PID=$!

# Give it time to start scanning then interrupt
sleep 1
kill -INT $STRACE_PID
wait $STRACE_PID 2>/dev/null || true

echo -e "\n--- Analyzing strace output ---"

# Check for signal handling
echo "Signal handling:"
grep -E "kill|sigaction|SIGINT" "$STRACE_FILE" | tail -5

# Check for cache index file operations
echo -e "\nCache index operations:"
grep -E "open.*cache.*\.idx|open.*cache.*\.tmp" "$STRACE_FILE" | tail -5

# Check for write operations to cache
echo -e "\nWrite operations to cache:"
grep -E "writev.*cache|write.*cache" "$STRACE_FILE" | grep -v "EBADF" | tail -5

# Check for rename operations
echo -e "\nRename operations:"
grep -E "rename.*cache" "$STRACE_FILE" | tail -5

# Check final state
echo -e "\n--- Final state ---"
ls -la .dcfh/

# Show if we have scan index files
echo -e "\nScan index files:"
ls -la .dcfh/scan-*.idx 2>/dev/null || echo "No scan index files found"

# Cleanup
rm -f "$STRACE_FILE"
cd /
rm -rf "$TEST_DIR"