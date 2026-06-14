package dircachefilehash

// FixEntryFlags carries the only CLI option keys the relocated dcfhfix entry
// repair/promote helpers actually read. It is a deliberately narrow relocation
// shim: cmd/dcfhfix builds one from its ParsedOptions at each call site, so the
// relocated repair workflow does not depend on the CLI option-parser type.
//
// It stays minimal on purpose. Task 28.2's public Fix surface (FixRequest /
// Options) subsumes the wider Backup/DryRun concerns; keeping this struct to
// the three flags the workflow actually consumes avoids colliding with that
// future surface.
type FixEntryFlags struct {
	Quiet       bool // suppress routine progress/warning output
	EditInPlace bool // overwrite the subject in place (no .pre-fix sibling)
	Force       bool // required opt-in for the destructive EditInPlace mode
}
