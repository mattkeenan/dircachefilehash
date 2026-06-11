package dircachefilehash

import (
	"errors"
	"os"
	"testing"
)

// errInjected is the shared error returned by the fault helpers below. Tests
// assert that this value (or a wrap of it) surfaces from the operation under
// test on the open/write/sync paths.
var errInjected = errors.New("injected fault")

// swapFn installs newFn into *target and restores the prior value on cleanup.
// The seam vars it drives are shared package state, so a test using swapFn must
// not call t.Parallel() (NFR5) — the default serial execution plus t.Cleanup
// restore keeps the swap window contained to the single test.
func swapFn[T any](t *testing.T, target *T, newFn T) {
	t.Helper()
	prev := *target
	*target = newFn
	t.Cleanup(func() { *target = prev })
}

// withRenameFault forces the atomic-replacement os.Rename to fail with err.
func withRenameFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsRename, func(_, _ string) error { return err })
}

// withOpenFault forces the temp-index os.OpenFile to fail with err.
func withOpenFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsOpenFile, func(_ string, _ int, _ os.FileMode) (*os.File, error) { return nil, err })
}

// withSyncFault forces the pre-rename file.Sync to fail with err.
func withSyncFault(t *testing.T, err error) {
	t.Helper()
	swapFn(t, &fsSync, func(_ *os.File) error { return err })
}

// withHashPreReadHook installs a pre-hash hook invoked by hashEntry with each
// entry's relative path, just before the file is read.
func withHashPreReadHook(t *testing.T, hook func(relPath string)) {
	t.Helper()
	swapFn(t, &hashPreReadHook, hook)
}
