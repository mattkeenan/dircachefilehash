package dircachefilehash

import (
	"fmt"
	"strings"
)

// hwangLinUnified implements the unified Hwang-Lin algorithm that works with
// any combination of PathEntryIterators and HwangLinCallbacks.
//
// This replaces the various specialized Hwang-Lin implementations throughout
// the codebase with a single, flexible algorithm that can handle:
// - Status checking (main vs scan)
// - Duplicate detection (any iterator vs any iterator)
// - Update operations (scan vs existing indices)
// - Any future comparison needs
//
// The algorithm maintains the same O(n+m) efficiency as the original
// Hwang-Lin algorithm while providing composable, testable components.
func hwangLinUnified(
	leftIter, rightIter PathEntryIterator,
	callback HwangLinCallback,
) error {
	if leftIter == nil || rightIter == nil || callback == nil {
		return fmt.Errorf("hwangLinUnified: nil parameters not allowed")
	}
	
	defer func() {
		// Always close iterators, even on early return
		leftIter.Close()
		rightIter.Close()
	}()
	
	// Initialize callback
	if err := callback.OnStart(leftIter.Name(), rightIter.Name()); err != nil {
		return fmt.Errorf("callback OnStart failed: %w", err)
	}
	
	// Get initial entries from both iterators
	leftEntry, leftErr := leftIter.Next()
	rightEntry, rightErr := rightIter.Next()
	
	// Handle initial errors
	if leftErr != nil {
		return fmt.Errorf("left iterator initial read failed: %w", leftErr)
	}
	if rightErr != nil {
		return fmt.Errorf("right iterator initial read failed: %w", rightErr)
	}
	
	// Main comparison loop - classic Hwang-Lin algorithm
	for leftEntry != nil || rightEntry != nil {
		var result ComparisonResult
		var leftPath, rightPath string
		var continueProcessing bool
		var err error
		
		// Get paths safely (handle nil entries)
		if leftEntry != nil {
			leftPath = leftEntry.RelativePath()
		}
		if rightEntry != nil {
			rightPath = rightEntry.RelativePath()
		}
		
		// Determine comparison result
		if leftEntry == nil {
			// Left exhausted, only right entries remain
			result = ComparisonRightOnly
			continueProcessing, err = callback.OnRightOnly(rightEntry, rightPath)
			if err != nil {
				callback.OnComplete(err)
				return err
			}
			if !continueProcessing {
				return callback.OnComplete(nil)
			}
			
			// Advance right iterator
			rightEntry, rightErr = rightIter.Next()
			if rightErr != nil {
				iterErr := fmt.Errorf("right iterator read failed: %w", rightErr)
				callback.OnComplete(iterErr)
				return iterErr
			}
			
		} else if rightEntry == nil {
			// Right exhausted, only left entries remain
			result = ComparisonLeftOnly
			continueProcessing, err = callback.OnLeftOnly(leftEntry, leftPath)
			if err != nil {
				callback.OnComplete(err)
				return err
			}
			if !continueProcessing {
				return callback.OnComplete(nil)
			}
			
			// Advance left iterator
			leftEntry, leftErr = leftIter.Next()
			if leftErr != nil {
				iterErr := fmt.Errorf("left iterator read failed: %w", leftErr)
				callback.OnComplete(iterErr)
				return iterErr
			}
			
		} else {
			// Both entries present - compare paths
			cmp := strings.Compare(leftPath, rightPath)
			
			if cmp == 0 {
				// Paths match
				result = ComparisonMatch
				continueProcessing, err = callback.OnComparison(result, leftEntry, rightEntry, leftPath, rightPath)
				if err != nil || !continueProcessing {
					callback.OnComplete(err)
					return err
				}
				
				// Advance both iterators
				leftEntry, leftErr = leftIter.Next()
				if leftErr != nil {
					iterErr := fmt.Errorf("left iterator read failed: %w", leftErr)
					callback.OnComplete(iterErr)
					return iterErr
				}
				rightEntry, rightErr = rightIter.Next()
				if rightErr != nil {
					iterErr := fmt.Errorf("right iterator read failed: %w", rightErr)
					callback.OnComplete(iterErr)
					return iterErr
				}
				
			} else if cmp < 0 {
				// Left entry comes first
				result = ComparisonLeftFirst
				continueProcessing, err = callback.OnComparison(result, leftEntry, nil, leftPath, "")
				if err != nil || !continueProcessing {
					callback.OnComplete(err)
					return err
				}
				
				// Advance left iterator only
				leftEntry, leftErr = leftIter.Next()
				if leftErr != nil {
					iterErr := fmt.Errorf("left iterator read failed: %w", leftErr)
					callback.OnComplete(iterErr)
					return iterErr
				}
				
			} else {
				// Right entry comes first
				result = ComparisonRightFirst
				continueProcessing, err = callback.OnComparison(result, nil, rightEntry, "", rightPath)
				if err != nil || !continueProcessing {
					callback.OnComplete(err)
					return err
				}
				
				// Advance right iterator only
				rightEntry, rightErr = rightIter.Next()
				if rightErr != nil {
					iterErr := fmt.Errorf("right iterator read failed: %w", rightErr)
					callback.OnComplete(iterErr)
					return iterErr
				}
			}
		}
	}
	
	// Algorithm completed successfully
	return callback.OnComplete(nil)
}

// Helper constants for convenience - these align with the callback interface
const (
	ComparisonLeftOnly  = ComparisonLeftExhausted
	ComparisonRightOnly = ComparisonRightExhausted
)