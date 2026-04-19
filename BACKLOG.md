# Backlog

## Separate discovery from explicit resolution in ResolveRepository

`ResolveRepository` mixes two fundamentally different operations: discovery (walk up from cwd looking for `.dcfh`) and explicit resolution (metaDir is already known, resolve rootDir from config). These should be split into `DiscoverRepository` (local filesystem walk) and `ResolveRepository` (known metaDir → rootDir). This separation is needed for future remote repo support via `--meta-dir ssh://host/path/repo.dcfh` — discovery is always local, but explicit resolution could be remote. See assessment: `.claude/plans/transient-wondering-conway.md`.
