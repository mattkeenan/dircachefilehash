package dircachefilehash

import (
	"testing"
)

func TestHwangLinUnified(t *testing.T) {
	t.Run("BasicComparison", func(t *testing.T) {
		// Create two sets of test entries
		leftEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file3.txt"),
			createMockBinaryEntry("file5.txt"),
		}
		
		rightEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file2.txt"),
			createMockBinaryEntry("file3.txt"),
			createMockBinaryEntry("file4.txt"),
		}
		
		// Create iterators
		leftIter := newMockIterator("left-iter", leftEntries)
		rightIter := newMockIterator("right-iter", rightEntries)
		
		// Create callback to record results
		callback := newMockCallback("test-callback")
		
		// Run unified algorithm
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified failed: %v", err)
		}
		
		// Verify expected call sequence
		expectedCalls := []string{
			"OnStart(left-iter, right-iter)",
			"OnComparison(LeftFirst, file1.txt, )",      // file1.txt only in left
			"OnComparison(RightFirst, , file2.txt)",     // file2.txt only in right
			"OnComparison(Match, file3.txt, file3.txt)", // file3.txt in both
			"OnComparison(RightFirst, , file4.txt)",     // file4.txt only in right (left has file5.txt)
			"OnLeftOnly(file5.txt)",                     // file5.txt only in left (right exhausted)
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
	
	t.Run("EmptyIterators", func(t *testing.T) {
		leftIter := newMockIterator("empty-left", []BinaryEntryInterface{})
		rightIter := newMockIterator("empty-right", []BinaryEntryInterface{})
		callback := newMockCallback("empty-callback")
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified failed: %v", err)
		}
		
		expectedCalls := []string{
			"OnStart(empty-left, empty-right)",
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
	
	t.Run("LeftOnlyEntries", func(t *testing.T) {
		leftEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
		}
		
		leftIter := newMockIterator("left-iter", leftEntries)
		rightIter := newMockIterator("empty-right", []BinaryEntryInterface{})
		callback := newMockCallback("left-only-callback")
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified failed: %v", err)
		}
		
		expectedCalls := []string{
			"OnStart(left-iter, empty-right)",
			"OnLeftOnly(file1.txt)",
			"OnLeftOnly(file2.txt)",
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
	
	t.Run("RightOnlyEntries", func(t *testing.T) {
		rightEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
		}
		
		leftIter := newMockIterator("empty-left", []BinaryEntryInterface{})
		rightIter := newMockIterator("right-iter", rightEntries)
		callback := newMockCallback("right-only-callback")
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified failed: %v", err)
		}
		
		expectedCalls := []string{
			"OnStart(empty-left, right-iter)",
			"OnRightOnly(file1.txt)",
			"OnRightOnly(file2.txt)",
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
	
	t.Run("EarlyStopFromCallback", func(t *testing.T) {
		leftEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
			createMockBinaryEntry("file3.txt"),
		}
		
		rightEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
			createMockBinaryEntry("file3.txt"),
		}
		
		leftIter := newMockIterator("left-iter", leftEntries)
		rightIter := newMockIterator("right-iter", rightEntries)
		callback := newMockCallback("stop-callback")
		
		// Configure callback to stop after 3 calls (OnStart + 2 OnComparison calls)
		callback.shouldStop = true
		callback.stopAfterCalls = 3
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified should not error on early stop: %v", err)
		}
		
		// Should stop after 2 comparison calls
		expectedCalls := []string{
			"OnStart(left-iter, right-iter)",
			"OnComparison(Match, file1.txt, file1.txt)",
			"OnComparison(Match, file2.txt, file2.txt)",
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d\nExpected: %v\nActual: %v", 
				len(expectedCalls), len(callback.calls), expectedCalls, callback.calls)
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
	
	t.Run("CallbackError", func(t *testing.T) {
		leftEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
		}
		
		rightEntries := []BinaryEntryInterface{
			createMockBinaryEntry("file1.txt"),
			createMockBinaryEntry("file2.txt"),
		}
		
		leftIter := newMockIterator("left-iter", leftEntries)
		rightIter := newMockIterator("right-iter", rightEntries)
		callback := newMockCallback("error-callback")
		
		// Configure callback to error after 1 call
		callback.shouldError = true
		callback.errorAfterCalls = 1
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err == nil {
			t.Fatal("hwangLinUnified should error when callback errors")
		}
		
		// Should have the error call and OnComplete with error
		expectedCalls := []string{
			"OnStart(left-iter, right-iter)",
			"OnComparison(Match, file1.txt, file1.txt)",
			"OnComplete(mock error after 1 calls)",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
	})
	
	t.Run("StartError", func(t *testing.T) {
		leftIter := newMockIterator("left-iter", []BinaryEntryInterface{})
		rightIter := newMockIterator("right-iter", []BinaryEntryInterface{})
		callback := newMockCallback("start-error-callback")
		
		callback.onStartError = true
		
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err == nil {
			t.Fatal("hwangLinUnified should error when OnStart errors")
		}
		
		if err.Error() != "callback OnStart failed: mock start error" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
	
	t.Run("NilParameters", func(t *testing.T) {
		leftIter := newMockIterator("left-iter", []BinaryEntryInterface{})
		rightIter := newMockIterator("right-iter", []BinaryEntryInterface{})
		callback := newMockCallback("callback")
		
		// Test nil left iterator
		err := hwangLinUnified(nil, rightIter, callback)
		if err == nil {
			t.Error("Should error with nil left iterator")
		}
		
		// Test nil right iterator
		err = hwangLinUnified(leftIter, nil, callback)
		if err == nil {
			t.Error("Should error with nil right iterator")
		}
		
		// Test nil callback
		err = hwangLinUnified(leftIter, rightIter, nil)
		if err == nil {
			t.Error("Should error with nil callback")
		}
	})
}

func TestHwangLinUnifiedWithSkiplists(t *testing.T) {
	t.Run("SkiplistIntegration", func(t *testing.T) {
		// Create test skiplists with different file sets
		leftPaths := []string{"file1.txt", "file3.txt", "file5.txt"}
		rightPaths := []string{"file2.txt", "file3.txt", "file4.txt"}
		
		leftSkiplist := createTestSkiplistWrapper(leftPaths)
		rightSkiplist := createTestSkiplistWrapper(rightPaths)
		
		// Create skiplist iterators
		leftIter := NewSkiplistIterator(leftSkiplist, "left-skiplist")
		rightIter := NewSkiplistIterator(rightSkiplist, "right-skiplist")
		
		// Create callback
		callback := newMockCallback("skiplist-callback")
		
		// Run unified algorithm
		err := hwangLinUnified(leftIter, rightIter, callback)
		if err != nil {
			t.Fatalf("hwangLinUnified with skiplists failed: %v", err)
		}
		
		// Verify expected call sequence (should be same as basic test but with skiplist iterators)
		expectedCalls := []string{
			"OnStart(left-skiplist, right-skiplist)",
			"OnComparison(LeftFirst, file1.txt, )",      // file1.txt only in left
			"OnComparison(RightFirst, , file2.txt)",     // file2.txt only in right
			"OnComparison(Match, file3.txt, file3.txt)", // file3.txt in both
			"OnComparison(RightFirst, , file4.txt)",     // file4.txt only in right (left has file5.txt)
			"OnLeftOnly(file5.txt)",                     // file5.txt only in left (right exhausted)
			"OnComplete()",
		}
		
		if len(callback.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(callback.calls))
		}
		
		for i, expected := range expectedCalls {
			if callback.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, callback.calls[i])
			}
		}
	})
}