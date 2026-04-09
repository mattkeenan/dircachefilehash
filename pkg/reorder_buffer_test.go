package dircachefilehash

import (
	"context"
	"testing"
)

// makeEntry creates a minimal PipelineEntry with the given sequence number.
func makeEntry(seq uint64) *PipelineEntry {
	return &PipelineEntry{SeqNum: seq, Operation: OpUnchanged}
}

func TestReorderBufferInOrder(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	bypass := make(chan *PipelineEntry, 10)
	hashed := make(chan *PipelineEntry) // unused

	for i := uint64(0); i < 5; i++ {
		bypass <- makeEntry(i)
	}
	close(bypass)
	close(hashed)

	err := rb.Run(context.Background(), bypass, hashed)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for i := uint64(0); i < 5; i++ {
		pe := <-output
		if pe.SeqNum != i {
			t.Errorf("expected SeqNum %d, got %d", i, pe.SeqNum)
		}
	}

	// Output should be closed
	_, ok := <-output
	if ok {
		t.Error("output channel should be closed")
	}
}

func TestReorderBufferOutOfOrder(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	bypass := make(chan *PipelineEntry, 10)
	hashed := make(chan *PipelineEntry, 10)

	// Send entries out of order across both channels
	hashed <- makeEntry(2)
	hashed <- makeEntry(4)
	bypass <- makeEntry(0)
	bypass <- makeEntry(1)
	hashed <- makeEntry(3)

	close(bypass)
	close(hashed)

	err := rb.Run(context.Background(), bypass, hashed)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for i := uint64(0); i < 5; i++ {
		pe := <-output
		if pe.SeqNum != i {
			t.Errorf("expected SeqNum %d, got %d", i, pe.SeqNum)
		}
	}
}

func TestReorderBufferMixedChannels(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	bypass := make(chan *PipelineEntry, 10)
	hashed := make(chan *PipelineEntry, 10)

	// Even numbers via bypass, odd via hashed
	bypass <- makeEntry(0)
	hashed <- makeEntry(1)
	bypass <- makeEntry(2)
	hashed <- makeEntry(3)

	close(bypass)
	close(hashed)

	err := rb.Run(context.Background(), bypass, hashed)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for i := uint64(0); i < 4; i++ {
		pe := <-output
		if pe.SeqNum != i {
			t.Errorf("expected SeqNum %d, got %d", i, pe.SeqNum)
		}
	}
}

func TestReorderBufferEmpty(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	bypass := make(chan *PipelineEntry)
	hashed := make(chan *PipelineEntry)
	close(bypass)
	close(hashed)

	err := rb.Run(context.Background(), bypass, hashed)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	_, ok := <-output
	if ok {
		t.Error("output channel should be closed for empty input")
	}
}

func TestReorderBufferContextCancellation(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	ctx, cancel := context.WithCancel(context.Background())

	// Create blocking channels (never receive)
	bypass := make(chan *PipelineEntry)
	hashed := make(chan *PipelineEntry)

	done := make(chan error, 1)
	go func() {
		done <- rb.Run(ctx, bypass, hashed)
	}()

	cancel()

	err := <-done
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestReorderBufferValidate(t *testing.T) {
	output := make(chan *PipelineEntry, 10)
	rb := newReorderBuffer(output)

	// Simulate a gap: send 0, 2 but not 1
	bypass := make(chan *PipelineEntry, 10)
	hashed := make(chan *PipelineEntry)

	bypass <- makeEntry(0)
	bypass <- makeEntry(2) // gap: 1 is missing
	close(bypass)
	close(hashed)

	_ = rb.Run(context.Background(), bypass, hashed)

	// Entry 0 should have flushed, entry 2 should be pending
	err := rb.Validate()
	if err == nil {
		t.Fatal("expected validation error for unflushed entries")
	}
}

func TestReorderBufferLargeSequence(t *testing.T) {
	const n = 1000
	output := make(chan *PipelineEntry, n)
	rb := newReorderBuffer(output)

	bypass := make(chan *PipelineEntry, n)
	hashed := make(chan *PipelineEntry)

	// Send in reverse order
	for i := n - 1; i >= 0; i-- {
		bypass <- makeEntry(uint64(i))
	}
	close(bypass)
	close(hashed)

	err := rb.Run(context.Background(), bypass, hashed)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	for i := uint64(0); i < n; i++ {
		pe := <-output
		if pe.SeqNum != i {
			t.Errorf("expected SeqNum %d, got %d", i, pe.SeqNum)
		}
	}

	if err := rb.Validate(); err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}
}
