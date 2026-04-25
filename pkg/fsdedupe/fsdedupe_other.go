//go:build !linux

package fsdedupe

import (
	"context"
	"fmt"
	"runtime"
)

func run(_ context.Context, _ []Group, _ Options) (*Result, error) {
	return nil, fmt.Errorf("%w (%s)", ErrUnsupported, runtime.GOOS)
}
