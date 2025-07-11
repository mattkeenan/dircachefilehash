package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"syscall"
	"time"
)

const unixTo1885 = 2682374400

func encodeWallTime(sec int64, nsec int64) uint64 {
	offsetSec := sec + unixTo1885
	wall := (uint64(offsetSec) << 30) | uint64(nsec)
	return wall
}

func timeFromWall(wall uint64) time.Time {
	nsec := int64(wall & 0x3FFFFFFF) // 30 bits for nanoseconds
	sec := int64(wall>>30) - unixTo1885
	return time.Unix(sec, nsec)
}

func main() {
	fmt.Println("Testing timestamp encoding/decoding precision...")
	fmt.Println()

	// Test 1: Check if nanosecond precision is preserved
	fmt.Println("Test 1: Nanosecond precision")
	testTimes := []time.Time{
		time.Now(),
		time.Unix(1736409239, 123456789),
		time.Unix(1736409239, 999999999),
		time.Unix(1736409239, 0),
		time.Unix(1736409239, 1),
	}

	for _, original := range testTimes {
		sec := original.Unix()
		nsec := original.UnixNano() % 1e9
		
		encoded := encodeWallTime(sec, nsec)
		decoded := timeFromWall(encoded)
		
		fmt.Printf("Original: %v (sec=%d, nsec=%d)\n", original, sec, nsec)
		fmt.Printf("Encoded:  0x%016X\n", encoded)
		fmt.Printf("Decoded:  %v\n", decoded)
		
		if !original.Equal(decoded) {
			fmt.Printf("ERROR: Times don't match!\n")
			fmt.Printf("  Diff: %v\n", decoded.Sub(original))
		}
		fmt.Println()
	}

	// Test 2: Check bit limitations - 30 bits for nanoseconds
	fmt.Println("Test 2: Nanosecond bit limitations (30 bits)")
	maxNsec := int64(1<<30 - 1) // Maximum value that fits in 30 bits
	fmt.Printf("Max nanoseconds in 30 bits: %d\n", maxNsec)
	fmt.Printf("Nanoseconds in 1 second:     %d\n", 1e9)
	fmt.Printf("Ratio: %.2f\n", float64(maxNsec)/1e9)
	
	if maxNsec >= 1e9 {
		fmt.Println("✓ 30 bits is sufficient for nanosecond storage")
	} else {
		fmt.Println("✗ ERROR: 30 bits is NOT sufficient for nanosecond storage!")
	}
	fmt.Println()

	// Test 3: End-to-end test with actual file stats
	fmt.Println("Test 3: End-to-end test with real file stats")
	
	// Create a temporary file
	tmpfile, err := ioutil.TempFile("", "timestamp_test_*.txt")
	if err != nil {
		fmt.Printf("Error creating temp file: %v\n", err)
		return
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.WriteString("test content")
	tmpfile.Close()

	// Get file stats
	info, err := os.Stat(tmpfile.Name())
	if err != nil {
		fmt.Printf("Error stating file: %v\n", err)
		return
	}
	
	stat := info.Sys().(*syscall.Stat_t)
	
	// Test both mtime and ctime
	fmt.Println("\nTesting mtime:")
	mtimeEncoded := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	mtimeDecoded := timeFromWall(mtimeEncoded)
	mtimeOriginal := time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec)
	
	fmt.Printf("Original mtime: %v (sec=%d, nsec=%d)\n", mtimeOriginal, stat.Mtim.Sec, stat.Mtim.Nsec)
	fmt.Printf("Encoded:        0x%016X\n", mtimeEncoded)
	fmt.Printf("Decoded:        %v\n", mtimeDecoded)
	
	if mtimeOriginal.Equal(mtimeDecoded) {
		fmt.Println("✓ mtime matches after encode/decode")
	} else {
		fmt.Printf("✗ mtime MISMATCH! Diff: %v\n", mtimeDecoded.Sub(mtimeOriginal))
	}

	fmt.Println("\nTesting ctime:")
	ctimeEncoded := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	ctimeDecoded := timeFromWall(ctimeEncoded)
	ctimeOriginal := time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	
	fmt.Printf("Original ctime: %v (sec=%d, nsec=%d)\n", ctimeOriginal, stat.Ctim.Sec, stat.Ctim.Nsec)
	fmt.Printf("Encoded:        0x%016X\n", ctimeEncoded)
	fmt.Printf("Decoded:        %v\n", ctimeDecoded)
	
	if ctimeOriginal.Equal(ctimeDecoded) {
		fmt.Println("✓ ctime matches after encode/decode")
	} else {
		fmt.Printf("✗ ctime MISMATCH! Diff: %v\n", ctimeDecoded.Sub(ctimeOriginal))
	}

	// Test 4: Test with multiple files to check for false positives
	fmt.Println("\nTest 4: Testing multiple files for false change detection")
	
	// Create multiple files with slight time differences
	var files []string
	for i := 0; i < 5; i++ {
		f, err := ioutil.TempFile("", fmt.Sprintf("timestamp_test_%d_*.txt", i))
		if err != nil {
			fmt.Printf("Error creating temp file %d: %v\n", i, err)
			continue
		}
		f.WriteString(fmt.Sprintf("test content %d", i))
		f.Close()
		files = append(files, f.Name())
		defer os.Remove(f.Name())
		
		// Small delay to ensure different timestamps
		time.Sleep(10 * time.Millisecond)
	}
	
	// Check each file twice to simulate isFileChangedFromScanned
	for _, fname := range files {
		info1, err := os.Stat(fname)
		if err != nil {
			continue
		}
		stat1 := info1.Sys().(*syscall.Stat_t)
		
		// Encode timestamps
		mtime1 := encodeWallTime(stat1.Mtim.Sec, stat1.Mtim.Nsec)
		ctime1 := encodeWallTime(stat1.Ctim.Sec, stat1.Ctim.Nsec)
		
		// Stat again (simulating a later scan)
		info2, err := os.Stat(fname)
		if err != nil {
			continue
		}
		stat2 := info2.Sys().(*syscall.Stat_t)
		
		// Encode timestamps again
		mtime2 := encodeWallTime(stat2.Mtim.Sec, stat2.Mtim.Nsec)
		ctime2 := encodeWallTime(stat2.Ctim.Sec, stat2.Ctim.Nsec)
		
		// Check if they match
		if mtime1 != mtime2 || ctime1 != ctime2 {
			fmt.Printf("✗ File %s detected as changed when it wasn't!\n", fname)
			fmt.Printf("  mtime1: 0x%016X, mtime2: 0x%016X\n", mtime1, mtime2)
			fmt.Printf("  ctime1: 0x%016X, ctime2: 0x%016X\n", ctime1, ctime2)
		} else {
			fmt.Printf("✓ File %s correctly detected as unchanged\n", fname)
		}
	}

	// Test 5: Check for precision loss with actual nanosecond values
	fmt.Println("\nTest 5: Testing nanosecond precision edge cases")
	edgeCases := []int64{
		0,
		1,
		999999999,
		1000000000 - 1,
		500000000,
		123456789,
	}
	
	for _, nsec := range edgeCases {
		sec := int64(1736409239)
		encoded := encodeWallTime(sec, nsec)
		decoded := timeFromWall(encoded)
		decodedNsec := decoded.UnixNano() % 1e9
		
		fmt.Printf("nsec=%d: ", nsec)
		if decodedNsec != nsec {
			fmt.Printf("✗ Precision loss! Got %d\n", decodedNsec)
		} else {
			fmt.Printf("✓ Preserved\n")
		}
	}

	// Test 6: File system timestamp granularity
	fmt.Println("\nTest 6: File system timestamp considerations")
	fmt.Println("Different file systems have different timestamp granularities:")
	fmt.Println("- ext4: 1 nanosecond")
	fmt.Println("- ext3: 1 second") 
	fmt.Println("- FAT32: 2 seconds")
	fmt.Println("- HFS+: 1 second")
	fmt.Println("- NTFS: 100 nanoseconds")
	fmt.Println("\nCurrent file system may round timestamps, causing false change detection.")
}