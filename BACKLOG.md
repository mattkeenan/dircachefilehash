# Backlog

## Migrate pkg/ shutdown from chan to context.Context

The `pkg/` layer passes `shutdownChan <-chan struct{}` through hash workers, scan pipelines, and Hwang-Lin comparisons. The Go-idiomatic approach since 1.16 is `context.Context`, which integrates with HTTP servers, database calls, gRPC, and the broader ecosystem. The cobra migration bridges the gap at the CLI boundary (context → channel), but long-term each `pkg/` function should switch from `<-chan struct{}` to `context.Context` independently.

**Files:** `pkg/scan.go`, `pkg/update.go`, `pkg/status.go`, `pkg/file.go`, `pkg/middleware.go`, and all callers of `shutdownChan`
