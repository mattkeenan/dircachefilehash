#!/bin/bash
set -e

echo "Testing dcfh hashing behavior..."
echo

# Create a test directory
TEST_DIR="test-excess-hashing-check"
rm -rf "$TEST_DIR"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

echo "Test directory: $TEST_DIR"
echo

# Initialize dcfh
echo "1. Initializing dcfh repository..."
../dcfh init .
echo

# Create some test files
echo "2. Creating test files..."
echo "content1" > file1.txt
echo "content2" > file2.txt
echo "content3" > file3.txt
mkdir subdir
echo "content4" > subdir/file4.txt
echo

# First update - all files should be hashed
echo "3. First update (all files should be hashed)..."
HASH_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash update 2>&1)
HASH_COUNT=$(echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | wc -l)
echo "Files hashed in first update: $HASH_COUNT"
echo "Sample output:"
echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | head -5
echo

# Second update without changes - no files should be hashed
echo "4. Second update without changes (no files should be hashed)..."
HASH_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash update 2>&1)
HASH_COUNT=$(echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | wc -l)
echo "Files hashed in second update: $HASH_COUNT"
if [ "$HASH_COUNT" -gt 0 ]; then
    echo "WARNING: Files were re-hashed without changes!"
    echo "Debug output:"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)"
fi
echo

# Touch a file to change its timestamp
echo "5. Touching file2.txt to change timestamp..."
touch file2.txt
echo

# Third update - only touched file should be hashed
echo "6. Third update (only file2.txt should be hashed)..."
HASH_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash update 2>&1)
HASH_COUNT=$(echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | wc -l)
echo "Files hashed in third update: $HASH_COUNT"
if [ "$HASH_COUNT" -eq 1 ]; then
    echo "✓ Correct: Only one file was hashed"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)"
else
    echo "✗ ERROR: Expected 1 file to be hashed, got $HASH_COUNT"
    echo "Debug output:"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)"
fi
echo

# Modify content of a file
echo "7. Modifying content of file3.txt..."
echo "new content3" > file3.txt
echo

# Fourth update - only modified file should be hashed
echo "8. Fourth update (only file3.txt should be hashed)..."
HASH_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash update 2>&1)
HASH_COUNT=$(echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | wc -l)
echo "Files hashed in fourth update: $HASH_COUNT"
if [ "$HASH_COUNT" -eq 1 ]; then
    echo "✓ Correct: Only one file was hashed"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)"
else
    echo "✗ ERROR: Expected 1 file to be hashed, got $HASH_COUNT"
    echo "Debug output:"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)"
fi
echo

# Test with cache update
echo "9. Testing cache update behavior..."
echo "Running status to trigger cache update..."
HASH_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash status 2>&1)
HASH_COUNT=$(echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | wc -l)
echo "Files hashed during status (cache update): $HASH_COUNT"
if [ "$HASH_COUNT" -gt 0 ]; then
    echo "Sample output:"
    echo "$HASH_OUTPUT" | grep -E "(Hashing file:|Submitting hash job|processFileJob)" | head -5
fi
echo

# Look for any files being detected as changed
echo "10. Checking which files are detected as changed..."
CHANGE_OUTPUT=$(../dcfh -vvv --debug=scan,scanning,hash update 2>&1)
echo "Files detected as changed:"
echo "$CHANGE_OUTPUT" | grep -E "(File modified|file changed|isFileChangedFromScanned)" || echo "No change detection messages found"
echo

# Cleanup
echo "11. Cleaning up..."
cd ..
rm -rf "$TEST_DIR"
echo "Done."