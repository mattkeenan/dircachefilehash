# Backlog

## Add DcfhDir field to DirectoryCache

Store `dcfhDir` explicitly on the `DirectoryCache` struct instead of discarding it after construction. Currently the `.dcfh` path is re-derived 14 times via `filepath.Dir(dc.IndexFile)`. An explicit field makes the separation between `RootDir` (what to scan) and `DcfhDir` (where indices live) clear in the data model — needed for future support of `.dcfh` outside the repo root.

**Files:** `pkg/util.go`, `pkg/dircache.go`, `pkg/scan.go`, `pkg/recovery.go`, `pkg/index.go`, `pkg/update.go`

## Investigate modified+deleted overlap in status output

Files with UTF-8 en-dash characters appear in both modified AND deleted lists in `dcfh status` output. Needs investigation with `dcfhfind` (now fixed).

## Fix slow pre-commit hook

`cmd/dcfh/` interruption tests take 40+ seconds under `go test -race`. The pre-commit hook runs `go test -race ./...` which includes these tests, making every commit slow.
