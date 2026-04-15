# Backlog

## Add DcfhDir field to DirectoryCache

Store `dcfhDir` explicitly on the `DirectoryCache` struct instead of discarding it after construction. Currently the `.dcfh` path is re-derived 14 times via `filepath.Dir(dc.IndexFile)`. An explicit field makes the separation between `RootDir` (what to scan) and `DcfhDir` (where indices live) clear in the data model — needed for future support of `.dcfh` outside the repo root.

**Files:** `pkg/util.go`, `pkg/dircache.go`, `pkg/scan.go`, `pkg/recovery.go`, `pkg/index.go`, `pkg/update.go`

## Migrate pkg/ shutdown from chan to context.Context

The `pkg/` layer passes `shutdownChan <-chan struct{}` through hash workers, scan pipelines, and Hwang-Lin comparisons. The Go-idiomatic approach since 1.16 is `context.Context`, which integrates with HTTP servers, database calls, gRPC, and the broader ecosystem. The cobra migration bridges the gap at the CLI boundary (context → channel), but long-term each `pkg/` function should switch from `<-chan struct{}` to `context.Context` independently.

**Files:** `pkg/scan.go`, `pkg/update.go`, `pkg/status.go`, `pkg/file.go`, `pkg/middleware.go`, and all callers of `shutdownChan`

## Fix slow pre-commit hook

`cmd/dcfh/` interruption tests take 40+ seconds under `go test -race`. The pre-commit hook runs `go test -race ./...` which includes these tests, making every commit slow.
