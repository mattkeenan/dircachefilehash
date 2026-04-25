//go:build !linux

package fsdedupe

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
)

// TestRunReturnsUnsupported is the load-bearing guarantee that the
// non-Linux build compiles and that Run on those platforms reports
// the capability gap via ErrUnsupported rather than panicking or
// returning a generic error. When golang.org/x/sys/unix expands
// IoctlFileDedupeRange to additional platforms, narrow the build
// tag here (and broaden fsdedupe_linux.go's tag) accordingly.
func TestRunReturnsUnsupported(t *testing.T) {
	got, err := Run(context.Background(), []Group{{Hash: "h", Files: []string{"a", "b"}}}, Options{})
	if err == nil {
		t.Fatalf("Run returned nil error on %s; want ErrUnsupported", runtime.GOOS)
	}
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run returned %v; want errors.Is(err, ErrUnsupported)", err)
	}
	if !strings.Contains(err.Error(), runtime.GOOS) {
		t.Errorf("Run error %q does not mention GOOS %q", err.Error(), runtime.GOOS)
	}
	if got != nil {
		t.Errorf("Run returned non-nil Result=%+v on unsupported platform", got)
	}
}
