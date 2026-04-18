# Backlog

## SnapshotRepository field redundancy

`SnapshotsDir` is always `filepath.Join(MetaDir, "snapshots")`. Either drop `MetaDir` and keep `SnapshotsDir`, or drop `SnapshotsDir` and derive it. Storing both is redundant state.

## Double config parse for external repos

`PersistentPreRunE` in `cmd/dcfh/root.go` calls `ResolveExternalRoot` (which calls `LoadConfig`) for discovery, then the command handler calls `OpenDirectoryCache` → `configureDirectoryCache` (which calls `LoadConfig` again). Could cache the first load and pass it through, but it couples discovery and initialisation.

## TOCTOU stat in ResolveExternalRoot

`pkg/config.go` does `os.Stat(configPath)` before `LoadConfig`. The stat avoids parsing config for non-dcfh directories during the walk (the common case), but `LoadConfig` already handles missing files gracefully. Could remove the stat and let `LoadConfig` fail directly.

## os.Getwd() in ResolveRepository

Always called when `startDir` is empty. CLI callers like `findDcfhRepo` could pass the cwd they already know, but currently none of them have it. Low priority since it's one syscall per command invocation.
