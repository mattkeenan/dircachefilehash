package dircachefilehash

import "os"

// ScanRun bundles the per-invocation instruments and scratch state used
// by territory-reading verbs (Diff, Apply, Groups). The Repo impl
// assembles one before each verb invocation; it propagates explicitly
// through the scan/pipeline machinery so DirectoryCache never has to
// know about instruments it doesn't own.
//
// (Named ScanRun rather than ScanContext to avoid colliding with the
// MainContext/CacheContext/ScanContext merge-tag string constants in
// constants.go — same word, unrelated concept.)
//
// Lifetime: a single ScanRun is owned by exactly one in-flight verb
// call. Scratch fields (filterEnt/filterCtx) are reused across the
// scan-walker hot path so a million-file scan stays allocation-free,
// but they assume a single goroutine drives the scan stage of any one
// call.
//
// Fields the metaphor would call "scan-synchronisation" (mutex,
// in-progress flag, last error) are *not* here — those are per-Repo
// state. The repo impl owns them and acquires the lock around the call.
type ScanRun struct {
	// Store is the .dcfh container the verb runs against. Held here
	// so scan-time helpers can reach repository identity (RootDir,
	// MetaDir, ignoreManager) without an extra parameter on every
	// signature. Never nil during a live verb call.
	Store *DirectoryCache

	Walker      Walker
	FileHasher  Hasher
	SymlinkMode string
	HashWorkers int

	// ScanIgnore is the scan-time --ignore predicate (nil = off). Set
	// by the repo impl's configureFilters; read by scanIgnoreDrops on
	// every walked path. Output-time evaluation in the comparison sink
	// is authoritative; this is a push-down optimisation only.
	ScanIgnore FilterExpr

	// scratch storage reused per chokepoint hit so a million-file scan
	// doesn't allocate a million adapters and contexts. Single-goroutine
	// only (see Lifetime note above).
	filterEnt scanFilterEntry
	filterCtx FilterContext
}

// scanRun returns a fresh ScanRun pointing at dc with default-local
// instruments and zero per-call state. Convenience for tests and
// internal helpers that need to drive scan-time code without a Repo
// impl in the loop. Production verb calls go through localRepo.scanRun
// which fills in the repo's per-call instrument values.
func (dc *DirectoryCache) scanRun() *ScanRun {
	return &ScanRun{
		Store:      dc,
		Walker:     &localWalker{},
		FileHasher: &localHasher{dc: dc},
	}
}

// scanIgnoreDrops evaluates the scan-time --ignore predicate against
// (relPath, info) and returns true when the entry should be filtered
// out. Errors from the predicate (hash predicates always; stat
// predicates when info is nil) are swallowed so we never drop on
// uncertainty — output-time evaluation in the comparison sink is
// authoritative.
func (sr *ScanRun) scanIgnoreDrops(relPath string, info os.FileInfo, where string) bool {
	if sr == nil || sr.ScanIgnore == nil {
		return false
	}
	sr.filterEnt.relPath = relPath
	sr.filterEnt.info = info
	matched, err := sr.ScanIgnore.Evaluate(&sr.filterEnt, &sr.filterCtx)
	if err != nil || !matched {
		return false
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "%s: ignoring path due to --ignore predicate: %s", where, relPath)
	}
	return true
}
