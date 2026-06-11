package dircachefilehash

import "os"

// Fault-injection seams for failure-path tests. These wrap the os-level
// primitives on the atomic index-replacement path and a pre-hash hook on the
// scan pipeline. They default to the real function (or nil) and are INERT in
// production. INVARIANT: never assigned outside _test.go — a production
// assignment would turn these into a runtime index-write override vector.
var (
	fsRename   = os.Rename       // func(old, new string) error
	fsOpenFile = os.OpenFile     // func(name string, flag int, perm os.FileMode) (*os.File, error)
	fsSync     = (*os.File).Sync // method expression: func(*os.File) error
)

// hashPreReadHook, when non-nil, is invoked by hashEntry just before the file is
// read, with the entry's relative path. Test-only injection point (nil in
// production); used to mutate a file deterministically between scan and hash.
var hashPreReadHook func(relPath string)
