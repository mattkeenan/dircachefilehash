package dircachefilehash

// ComparisonResult represents the result of comparing two entries in the Hwang-Lin algorithm
type ComparisonResult int

const (
	// Both entries match (same path)
	ComparisonMatch ComparisonResult = iota
	// Left entry comes before right entry (left < right)
	ComparisonLeftFirst
	// Right entry comes before left entry (left > right)
	ComparisonRightFirst
	// Left iterator exhausted, only right entries remain
	ComparisonLeftExhausted
	// Right iterator exhausted, only left entries remain
	ComparisonRightExhausted
)

// String returns a string representation of the ComparisonResult
func (cr ComparisonResult) String() string {
	switch cr {
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
		return "Unknown"
	}
}

// HwangLinCallback defines the interface for operations that can be performed during
// Hwang-Lin comparison. This allows different algorithms (status, dupes, update) to
// plug into the unified comparison logic.
//
// The unified algorithm will call these methods as it compares entries from two
// sorted iterators, allowing the callback to perform its specific operation.
type HwangLinCallback interface {
	// OnComparison is called for each comparison result during Hwang-Lin algorithm.
	//
	// Parameters:
	// - result: The comparison result (match, left first, right first, etc.)
	// - leftEntry: Entry from left iterator (nil if right iterator has advanced)
	// - rightEntry: Entry from right iterator (nil if left iterator has advanced)
	// - leftPath: Path from left entry (provided for convenience and safety)
	// - rightPath: Path from right entry (provided for convenience and safety)
	//
	// Returns:
	// - continueProcessing: false to stop the algorithm early
	// - error: any error that occurred during processing
	OnComparison(
		result ComparisonResult,
		leftEntry, rightEntry BinaryEntryInterface,
		leftPath, rightPath string,
	) (continueProcessing bool, err error)

	// OnLeftOnly is called when the left iterator has entries but right is exhausted.
	// This is a convenience method for handling remaining entries from one side.
	OnLeftOnly(entry BinaryEntryInterface, path string) (continueProcessing bool, err error)

	// OnRightOnly is called when the right iterator has entries but left is exhausted.
	// This is a convenience method for handling remaining entries from one side.
	OnRightOnly(entry BinaryEntryInterface, path string) (continueProcessing bool, err error)

	// OnStart is called before the algorithm begins processing.
	// This allows callbacks to initialize state, validate inputs, etc.
	OnStart(leftName, rightName string) error

	// OnComplete is called after the algorithm finishes processing.
	// This allows callbacks to finalize results, cleanup resources, etc.
	// The error parameter indicates if the algorithm stopped due to an error.
	OnComplete(err error) error

	// SubmitAndOrWriteHash handles unified hash coordination and cache writing.
	// This method encapsulates all hash job submission, completion processing,
	// and iterative cache writing logic in one place.
	//
	// Parameters:
	// - entry: The BinaryEntryInterface that may need hashing and/or writing
	// - operation: Description of the operation context ("new_file", "modified", "unchanged", etc.)
	//
	// Implementation varies by callback:
	// - StatusCallback: Full hash coordination + cache.idx writing
	// - UpdateCallback: Full hash coordination + main.idx writing
	//
	// This method should be called by ALL On* methods before returning.
	SubmitAndOrWriteHash(entry BinaryEntryInterface, operation string) error

	// Name returns a descriptive name for this callback (for debugging/logging).
	Name() string
}

// CallbackBase provides common functionality for callback implementations
type CallbackBase struct {
	name string
}

// Name returns the callback name
func (cb *CallbackBase) Name() string {
	return cb.name
}

// OnStart provides a default implementation that does nothing
func (cb *CallbackBase) OnStart(leftName, rightName string) error {
	return nil
}

// OnComplete provides a default implementation that does nothing
func (cb *CallbackBase) OnComplete(err error) error {
	return nil
}

// OnLeftOnly provides a default implementation that continues processing
func (cb *CallbackBase) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	return true, nil
}

// OnRightOnly provides a default implementation that continues processing
func (cb *CallbackBase) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	return true, nil
}
