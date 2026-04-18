# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## Rejected

### Double config parse for external repos

Evaluated 2026-04-18: for external repos, `LoadConfig` is called in `ResolveExternalRoot` (discovery) and again in `configureDirectoryCache` (initialisation). Fixing would require changing 6 function signatures (including the public `OpenDirectoryCache` API) and adding global state to thread a `*Config` through the discovery layer. The config file is typically under 20 lines of INI — the extra parse takes microseconds and only affects external repos. The coupling cost exceeds the benefit.

### SnapshotRepository field redundancy

Evaluated 2026-04-18: `SnapshotsDir` is derivable from `MetaDir`. Dropping either field makes call sites less readable — `filepath.Dir(sr.SnapshotsDir)` obscures intent, `filepath.Join(sr.MetaDir, "snapshots")` repeated 8 times adds noise. The struct is created once per command. Both fields exist for readability, not due to a design flaw.

## [0.8.0] - 2026-04-15

Migrate CLI from custom option parser to cobra/viper. GNU longopt support (`--option value`), built-in shell completion (`dcfh completion [bash|zsh]`), viper config binding, and `--version` flag.

## [0.7.9] - 2026-04-14

Fix `BESkiplistEntry.RelativePath()` truncating paths longer than 256 bytes.

## [0.7.8] - 2026-04-14

Split `NewDirectoryCache` into `CreateDirectoryCache` and `OpenDirectoryCache` with clear semantics.

## [0.7.7] - 2026-04-14

Fix: use real ctime from stat instead of mtime for `BEScanEntry`.

## [0.7.6] - 2026-04-14

Show file sizes in status summary output.

## [0.7.5] - 2026-04-14

Fix O(n^2) skiplist iterator (cursor-based instead of restart-from-beginning). Add plain-bool fast path to `IsDebugEnabled`.

## [0.7.4] - 2026-04-14

Remove redundant `shouldIndex` calls and per-entry mutex from `BEScanEntry`.

## [0.7.3] - 2026-04-13

Migrate status command to channel-based pipeline architecture.

## [0.7.2] - 2026-04-13

Add `dcfh subrepo` command scaffold. Unified JSON output. Skip `.git` internals during scan.

## [0.7.1] - 2026-04-13

Reduce default hash workers from 4 to 2.

## [0.7.0] - 2026-04-13

Major architecture rewrite: unified `BinaryEntryInterface` system with `BESkiplistEntry`, `BEScanEntry`, and `BEIndexFileEntry`. Channel-based pipeline, two-phase hash coordination, `TempIndexWriter` with IoVec batching, per-worker hash buffer reuse. Fix signal handling livelock and empty `.deb` packages.

## [0.6.5] and earlier

See git history.
