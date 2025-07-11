#!/bin/bash
# Test hash performance with instrumentation

set -e

# Create a test directory in the git repo (on real disk, not tmpfs)
REPO_ROOT="$(git -C /home/matt/repo/dircachefilehash rev-parse --show-toplevel)"
TEST_DIR="$REPO_ROOT/test-hash-perf-$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Creating test files..."
# Create files of different sizes
dd if=/dev/urandom of=small_1KB.bin bs=1024 count=1 2>/dev/null
dd if=/dev/urandom of=medium_1MB.bin bs=1024 count=1024 2>/dev/null
dd if=/dev/urandom of=large_10MB.bin bs=1024 count=10240 2>/dev/null
dd if=/dev/urandom of=xlarge_100MB.bin bs=1024 count=102400 2>/dev/null

# Initialize dcfh
echo "Initializing dcfh..."
"$REPO_ROOT/dcfh" init .

# Sync to ensure files are written to disk
sync

# Drop page cache (requires sudo)
echo -e "\nDropping page cache to test real disk performance..."
if sudo -n true 2>/dev/null; then
    sudo sh -c 'echo 3 > /proc/sys/vm/drop_caches'
    echo "Page cache dropped"
else
    echo "Warning: Cannot drop page cache without sudo. Results will reflect cached performance."
fi

# Run status with hash debug enabled and verbose level 3
echo -e "\n=== Running status with hash performance instrumentation ==="
"$REPO_ROOT/dcfh" -vvv --debug=hash status

# Cleanup
cd /
rm -rf "$TEST_DIR"