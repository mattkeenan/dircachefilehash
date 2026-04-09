package dircachefilehash

import (
	"context"
	"fmt"
)

// reorderBuffer receives PipelineEntry values from two input channels
// (bypass and hashed) and emits them in strict SeqNum order to the output
// channel. It runs in a single goroutine, so its internal map requires no
// synchronisation.
type reorderBuffer struct {
	pending    map[uint64]*PipelineEntry
	nextSeqNum uint64
	output     chan<- *PipelineEntry
}

// newReorderBuffer creates a reorder buffer that writes to output.
// The caller must close output only after Run returns.
func newReorderBuffer(output chan<- *PipelineEntry) *reorderBuffer {
	return &reorderBuffer{
		pending:    make(map[uint64]*PipelineEntry),
		nextSeqNum: 0,
		output:     output,
	}
}

// Run reads from bypassCh and hashedCh until both are closed (or ctx is
// cancelled), flushing entries to the output channel in strict SeqNum order.
// It closes the output channel before returning.
func (rb *reorderBuffer) Run(ctx context.Context, bypassCh, hashedCh <-chan *PipelineEntry) error {
	defer close(rb.output)

	for bypassCh != nil || hashedCh != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case pe, ok := <-bypassCh:
			if !ok {
				bypassCh = nil
				continue
			}
			rb.pending[pe.SeqNum] = pe
			if err := rb.flush(ctx); err != nil {
				return err
			}

		case pe, ok := <-hashedCh:
			if !ok {
				hashedCh = nil
				continue
			}
			rb.pending[pe.SeqNum] = pe
			if err := rb.flush(ctx); err != nil {
				return err
			}
		}
	}

	// Drain any remaining buffered entries after both inputs close.
	return rb.flush(ctx)
}

// flush sends all contiguous entries starting from nextSeqNum to the output
// channel. Returns on gap or context cancellation.
func (rb *reorderBuffer) flush(ctx context.Context) error {
	for {
		pe, ok := rb.pending[rb.nextSeqNum]
		if !ok {
			return nil
		}
		delete(rb.pending, rb.nextSeqNum)
		rb.nextSeqNum++

		select {
		case <-ctx.Done():
			return ctx.Err()
		case rb.output <- pe:
		}
	}
}

// Pending returns the number of entries buffered but not yet flushed.
// Intended for testing and diagnostics.
func (rb *reorderBuffer) Pending() int {
	return len(rb.pending)
}

// Validate checks that all entries have been flushed. Returns an error if
// entries remain in the pending map (indicating a gap in the sequence that
// was never filled).
func (rb *reorderBuffer) Validate() error {
	if len(rb.pending) > 0 {
		return fmt.Errorf("reorder buffer has %d unflushed entries (next expected SeqNum: %d)", len(rb.pending), rb.nextSeqNum)
	}
	return nil
}
