package dircachefilehash

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IndexRef identifies a resolved index file or virtual data source together
// with the selector that produced it. See pkg/openref.go for the full Type
// vocabulary recognised by OpenRef.
type IndexRef struct {
	Path       string
	Type       string
	ScanID     string // Type=="scan": "PID-TID"
	SnapshotID string // Type=="snapshot": exact ID or tag
}

// ResolveIndexSelectors turns a list of dcfhfind-style selectors (main, cache,
// scan, all, scan-<pid>-<tid>, or direct paths) into a deduplicated list of
// IndexRefs that exist on disk. Missing files are silently skipped, matching
// find(1) semantics.
func ResolveIndexSelectors(metaDir string, selectors []string) ([]IndexRef, error) {
	var out []IndexRef
	for _, sel := range selectors {
		refs, err := resolveOneSelector(metaDir, sel)
		if err != nil {
			return nil, err
		}
		out = append(out, refs...)
	}

	seen := make(map[string]bool, len(out))
	filtered := out[:0]
	for _, r := range out {
		key := r.Path
		if key == "" {
			// Virtual refs (cache+main, fs-scan, snapshot) carry no path —
			// dedupe on (Type, SnapshotID) instead.
			key = r.Type + ":" + r.SnapshotID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		if r.Path != "" {
			if _, err := os.Stat(r.Path); os.IsNotExist(err) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

// ParseIndexRef parses a single selector string (the same vocabulary as
// ResolveIndexSelectors) into an IndexRef without performing any
// existence checks. Errors when the selector resolves to multiple refs
// (e.g. bare "scan" when several scan files exist) — diff and snapshot
// status need exactly one ref per side.
func ParseIndexRef(metaDir, sel string) (IndexRef, error) {
	refs, err := resolveOneSelector(metaDir, sel)
	if err != nil {
		return IndexRef{}, err
	}
	if len(refs) != 1 {
		return IndexRef{}, fmt.Errorf("selector %q resolved to %d refs (expected exactly 1)", sel, len(refs))
	}
	return refs[0], nil
}

func resolveOneSelector(metaDir, sel string) ([]IndexRef, error) {
	switch sel {
	case "main":
		return []IndexRef{{Path: filepath.Join(metaDir, MainIndex), Type: RefTypeMain}}, nil
	case "cache":
		return []IndexRef{{Path: filepath.Join(metaDir, CacheIndex), Type: RefTypeCache}}, nil
	case "scan":
		matches, err := filepath.Glob(filepath.Join(metaDir, "scan-*.idx"))
		if err != nil {
			return nil, fmt.Errorf("error finding scan files: %w", err)
		}
		var refs []IndexRef
		for _, m := range matches {
			base := filepath.Base(m)
			if strings.HasPrefix(base, "scan-") && strings.HasSuffix(base, ".idx") {
				refs = append(refs, IndexRef{
					Path:   m,
					Type:   RefTypeScan,
					ScanID: base[5 : len(base)-4],
				})
			}
		}
		return refs, nil
	case "all":
		var refs []IndexRef
		for _, s := range []string{"main", "cache", "scan"} {
			sub, err := resolveOneSelector(metaDir, s)
			if err != nil {
				continue
			}
			refs = append(refs, sub...)
		}
		return refs, nil
	case "cache+main":
		return []IndexRef{{Type: RefTypeCacheMain}}, nil
	case "fs-scan":
		return []IndexRef{{Type: RefTypeFsScan}}, nil
	}

	if id, ok := strings.CutPrefix(sel, "snapshot:"); ok {
		if id == "" {
			return nil, fmt.Errorf("snapshot: requires an id or tag (e.g. snapshot:monthly)")
		}
		return []IndexRef{{Type: RefTypeSnapshot, SnapshotID: id}}, nil
	}

	if strings.HasPrefix(sel, "scan-") && (strings.Contains(sel, "-") || strings.HasSuffix(sel, ".idx")) {
		path := sel
		if !strings.HasSuffix(path, ".idx") {
			path += ".idx"
		}
		path = filepath.Join(metaDir, path)
		base := filepath.Base(path)
		if strings.HasPrefix(base, "scan-") && strings.HasSuffix(base, ".idx") {
			return []IndexRef{{
				Path:   path,
				Type:   RefTypeScan,
				ScanID: base[5 : len(base)-4],
			}}, nil
		}
	}
	return []IndexRef{{Path: sel, Type: RefTypeFile}}, nil
}

// FilterRequest selects which indices to scan and supplies the predicate tree
// and actions that execute on each matching entry. Expression may be nil (match
// everything). Actions must be non-empty; use PrintAction for default behaviour.
type FilterRequest struct {
	Options        Options        `json:"options"`
	IndexSelectors []string       `json:"index_selectors"`
	Repository     string         `json:"repository,omitempty"`
	Expression     FilterExpr     `json:"-"`
	Actions        []FilterAction `json:"-"`
	Warn           bool           `json:"warn,omitempty"`
}

// FilterResult summarises Filter execution.
type FilterResult struct {
	IndexFilesSearched int `json:"index_files_searched"`
	EntriesMatched     int `json:"entries_matched"`
}

// RunFilter iterates the resolved index files, evaluates the predicate against
// each entry, and executes the actions on matches. Errors from individual
// index files are written to warnOut (if non-nil and req.Warn is true) rather
// than aborting the whole run.
func RunFilter(ctx context.Context, refs []IndexRef, req FilterRequest, warnOut io.Writer) (*FilterResult, error) {
	result := &FilterResult{}
	for _, ref := range refs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		matched, err := runFilterOnIndex(ref, req)
		result.IndexFilesSearched++
		result.EntriesMatched += matched
		if err != nil && req.Warn && warnOut != nil {
			fmt.Fprintf(warnOut, "dcfhfind: warning: %s: %v\n", ref.Path, err)
		}
	}
	return result, nil
}

func runFilterOnIndex(ref IndexRef, req FilterRequest) (int, error) {
	matched := 0
	// Hoisted: IndexPath/Repository are loop-invariant across millions
	// of entries. IndexType is overridden per ref when set; otherwise
	// each callback supplies it (always the same value within one
	// IterateIndexFile call). EntryPath/RelativePath are the only
	// per-entry fields, mutated in place.
	ctx := &FilterContext{
		IndexPath:  ref.Path,
		Repository: req.Repository,
	}
	override := ref.Type != "" && ref.Type != "file"
	if override {
		ctx.IndexType = ref.Type
	}
	err := IterateIndexFile(ref.Path, func(entry *EntryInfo, indexType string) bool {
		if !override {
			ctx.IndexType = indexType
		}
		ctx.EntryPath = entry.Path
		ctx.RelativePath = entry.Path
		fe := entry.AsFilterEntry()
		if req.Expression != nil {
			ok, err := req.Expression.Evaluate(fe, ctx)
			if err != nil {
				if req.Warn {
					fmt.Fprintf(os.Stderr, "dcfhfind: warning: %s: %v\n", entry.Path, err)
				}
				return true
			}
			if !ok {
				return true
			}
		}
		matched++
		for _, action := range req.Actions {
			if err := action.Execute(fe, ctx); err != nil && req.Warn {
				fmt.Fprintf(os.Stderr, "dcfhfind: warning: action failed for %s: %v\n", entry.Path, err)
			}
		}
		return true
	})
	return matched, err
}
