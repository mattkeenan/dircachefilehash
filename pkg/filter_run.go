package dircachefilehash

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IndexRef identifies a resolved index file together with the selector
// that produced it ("main", "cache", "scan", or "file" for direct paths).
type IndexRef struct {
	Path   string
	Type   string
	ScanID string // for scan files: "PID-TID"
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
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		if _, err := os.Stat(r.Path); os.IsNotExist(err) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

func resolveOneSelector(metaDir, sel string) ([]IndexRef, error) {
	switch sel {
	case "main":
		return []IndexRef{{Path: filepath.Join(metaDir, MainIndex), Type: "main"}}, nil
	case "cache":
		return []IndexRef{{Path: filepath.Join(metaDir, CacheIndex), Type: "cache"}}, nil
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
					Type:   "scan",
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
				Type:   "scan",
				ScanID: base[5 : len(base)-4],
			}}, nil
		}
	}
	return []IndexRef{{Path: sel, Type: "file"}}, nil
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
	err := IterateIndexFile(ref.Path, func(entry *EntryInfo, indexType string) bool {
		if ref.Type != "" && ref.Type != "file" {
			indexType = ref.Type
		}
		ctx := &FilterContext{
			IndexPath:    ref.Path,
			IndexType:    indexType,
			Repository:   req.Repository,
			EntryPath:    entry.Path,
			RelativePath: entry.Path,
		}
		if req.Expression != nil {
			ok, err := req.Expression.Evaluate(entry, ctx)
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
			if err := action.Execute(entry, ctx); err != nil && req.Warn {
				fmt.Fprintf(os.Stderr, "dcfhfind: warning: action failed for %s: %v\n", entry.Path, err)
			}
		}
		return true
	})
	return matched, err
}
