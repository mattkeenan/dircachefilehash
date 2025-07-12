package dircachefilehash

import (
	"fmt"
	"testing"
)

// mockCallback implements HwangLinCallback for testing
type mockCallback struct {
	CallbackBase
	calls              []string
	shouldStop         bool
	shouldError        bool
	stopAfterCalls     int
	errorAfterCalls    int
	onStartError       bool
	onCompleteError    bool
}

// newMockCallback creates a mock callback with predefined behavior
func newMockCallback(name string) *mockCallback {
	return &mockCallback{
		CallbackBase: CallbackBase{name: name},
		calls:        make([]string, 0),
	}
}

// OnComparison records the call and optionally stops or errors
func (mc *mockCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	callDesc := fmt.Sprintf("OnComparison(%s, %s, %s)", 
		comparisonResultString(result), leftPath, rightPath)
	mc.calls = append(mc.calls, callDesc)
	
	if mc.shouldError && len(mc.calls) >= mc.errorAfterCalls {
		return false, fmt.Errorf("mock error after %d calls", len(mc.calls))
	}
	
	if mc.shouldStop && len(mc.calls) >= mc.stopAfterCalls {
		return false, nil
	}
	
	return true, nil
}

// OnLeftOnly records the call
func (mc *mockCallback) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	callDesc := fmt.Sprintf("OnLeftOnly(%s)", path)
	mc.calls = append(mc.calls, callDesc)
	return true, nil
}

// OnRightOnly records the call
func (mc *mockCallback) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	callDesc := fmt.Sprintf("OnRightOnly(%s)", path)
	mc.calls = append(mc.calls, callDesc)
	return true, nil
}

// OnStart records the call and optionally errors
func (mc *mockCallback) OnStart(leftName, rightName string) error {
	callDesc := fmt.Sprintf("OnStart(%s, %s)", leftName, rightName)
	mc.calls = append(mc.calls, callDesc)
	
	if mc.onStartError {
		return fmt.Errorf("mock start error")
	}
	
	return nil
}

// OnComplete records the call and optionally errors
func (mc *mockCallback) OnComplete(err error) error {
	var errStr string
	if err != nil {
		errStr = err.Error()
	}
	callDesc := fmt.Sprintf("OnComplete(%s)", errStr)
	mc.calls = append(mc.calls, callDesc)
	
	if mc.onCompleteError {
		return fmt.Errorf("mock complete error")
	}
	
	return nil
}

// Helper function to convert ComparisonResult to string
func comparisonResultString(result ComparisonResult) string {
	switch result {
	case ComparisonMatch:
		return "Match"
	case ComparisonLeftFirst:
		return "LeftFirst"
	case ComparisonRightFirst:
		return "RightFirst"
	case ComparisonLeftExhausted:
		return "LeftExhausted"
	case ComparisonRightExhausted:
		return "RightExhausted"
	default:
		return fmt.Sprintf("Unknown(%d)", int(result))
	}
}

func TestCallbackBase(t *testing.T) {
	t.Run("BasicFunctionality", func(t *testing.T) {
		cb := &CallbackBase{name: "test-callback"}
		
		if cb.Name() != "test-callback" {
			t.Errorf("Expected name 'test-callback', got '%s'", cb.Name())
		}
		
		// Test default implementations
		if err := cb.OnStart("left", "right"); err != nil {
			t.Errorf("OnStart should not error by default: %v", err)
		}
		
		if err := cb.OnComplete(nil); err != nil {
			t.Errorf("OnComplete should not error by default: %v", err)
		}
		
		if err := cb.OnComplete(fmt.Errorf("test error")); err != nil {
			t.Errorf("OnComplete should not error by default even with input error: %v", err)
		}
		
		cont, err := cb.OnLeftOnly(nil, "test.txt")
		if err != nil || !cont {
			t.Errorf("OnLeftOnly should continue and not error by default: cont=%t, err=%v", cont, err)
		}
		
		cont, err = cb.OnRightOnly(nil, "test.txt")
		if err != nil || !cont {
			t.Errorf("OnRightOnly should continue and not error by default: cont=%t, err=%v", cont, err)
		}
	})
}

func TestMockCallback(t *testing.T) {
	t.Run("BasicCallRecording", func(t *testing.T) {
		mock := newMockCallback("test-mock")
		
		// Test name
		if mock.Name() != "test-mock" {
			t.Errorf("Expected name 'test-mock', got '%s'", mock.Name())
		}
		
		// Test call recording
		err := mock.OnStart("left-iter", "right-iter")
		if err != nil {
			t.Fatalf("OnStart should not error: %v", err)
		}
		
		cont, err := mock.OnComparison(ComparisonMatch, nil, nil, "file1.txt", "file1.txt")
		if err != nil || !cont {
			t.Fatalf("OnComparison should continue and not error: cont=%t, err=%v", cont, err)
		}
		
		cont, err = mock.OnLeftOnly(nil, "file2.txt")
		if err != nil || !cont {
			t.Fatalf("OnLeftOnly should continue and not error: cont=%t, err=%v", cont, err)
		}
		
		cont, err = mock.OnRightOnly(nil, "file3.txt")
		if err != nil || !cont {
			t.Fatalf("OnRightOnly should continue and not error: cont=%t, err=%v", cont, err)
		}
		
		err = mock.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete should not error: %v", err)
		}
		
		// Verify recorded calls
		expectedCalls := []string{
			"OnStart(left-iter, right-iter)",
			"OnComparison(Match, file1.txt, file1.txt)",
			"OnLeftOnly(file2.txt)",
			"OnRightOnly(file3.txt)",
			"OnComplete()",
		}
		
		if len(mock.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(mock.calls))
		}
		
		for i, expected := range expectedCalls {
			if mock.calls[i] != expected {
				t.Errorf("Call %d: expected '%s', got '%s'", i, expected, mock.calls[i])
			}
		}
	})
	
	t.Run("EarlyStop", func(t *testing.T) {
		mock := newMockCallback("stop-mock")
		mock.shouldStop = true
		mock.stopAfterCalls = 2
		
		// First call should continue
		cont, err := mock.OnComparison(ComparisonMatch, nil, nil, "file1.txt", "file1.txt")
		if err != nil || !cont {
			t.Fatalf("First call should continue: cont=%t, err=%v", cont, err)
		}
		
		// Second call should stop
		cont, err = mock.OnComparison(ComparisonLeftFirst, nil, nil, "file2.txt", "")
		if err != nil || cont {
			t.Fatalf("Second call should stop: cont=%t, err=%v", cont, err)
		}
		
		// Verify calls
		expectedCalls := []string{
			"OnComparison(Match, file1.txt, file1.txt)",
			"OnComparison(LeftFirst, file2.txt, )",
		}
		
		if len(mock.calls) != len(expectedCalls) {
			t.Fatalf("Expected %d calls, got %d", len(expectedCalls), len(mock.calls))
		}
	})
	
	t.Run("ErrorHandling", func(t *testing.T) {
		mock := newMockCallback("error-mock")
		mock.shouldError = true
		mock.errorAfterCalls = 1
		
		// First call should error
		cont, err := mock.OnComparison(ComparisonMatch, nil, nil, "file1.txt", "file1.txt")
		if err == nil {
			t.Fatal("First call should error")
		}
		if cont {
			t.Fatal("Call should not continue when erroring")
		}
		
		if err.Error() != "mock error after 1 calls" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
	
	t.Run("StartError", func(t *testing.T) {
		mock := newMockCallback("start-error-mock")
		mock.onStartError = true
		
		err := mock.OnStart("left", "right")
		if err == nil {
			t.Fatal("OnStart should error")
		}
		
		if err.Error() != "mock start error" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
	
	t.Run("CompleteError", func(t *testing.T) {
		mock := newMockCallback("complete-error-mock")
		mock.onCompleteError = true
		
		err := mock.OnComplete(nil)
		if err == nil {
			t.Fatal("OnComplete should error")
		}
		
		if err.Error() != "mock complete error" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
}

func TestComparisonResultString(t *testing.T) {
	tests := []struct {
		result   ComparisonResult
		expected string
	}{
		{ComparisonMatch, "Match"},
		{ComparisonLeftFirst, "LeftFirst"},
		{ComparisonRightFirst, "RightFirst"},
		{ComparisonLeftExhausted, "LeftExhausted"},
		{ComparisonRightExhausted, "RightExhausted"},
		{ComparisonResult(999), "Unknown(999)"},
	}
	
	for _, test := range tests {
		actual := comparisonResultString(test.result)
		if actual != test.expected {
			t.Errorf("comparisonResultString(%d): expected '%s', got '%s'", 
				int(test.result), test.expected, actual)
		}
	}
}