# Documentation

This directory holds the architecture and design documentation for
`dircachefilehash`. Each document is tagged **Current** (verified against the
shipped code) or **Historical** (records a superseded design or proposal, kept
for context). Historical documents also carry a banner at the top.

## Architecture & design

| Document | Status | What it covers |
|----------|--------|----------------|
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | **Current** | The canonical architecture overview: the five layers, the three index file types, and the core system metaphors (Hwang-Lin merge, the four-stage pipeline, status-writes-cache, atomic temp+rename). Start here. |
| [`ARCHITECTURE-IMPROVEMENTS.md`](ARCHITECTURE-IMPROVEMENTS.md) | **Current** | Known rough edges in the current architecture, each naming a specific file/line. |
| [`architecture-v0.7.md`](architecture-v0.7.md) | **Historical** | The v0.6→v0.7 migration design spec. The shipped scan path is a four-stage channel pipeline, not the callback/iterative-write model described here. |
| [`streaming-iterator-architecture.md`](streaming-iterator-architecture.md) | **Historical** | A proposal for iterator↔hash-job notification coordination that was not shipped as described. |
| [`design.md`](design.md) | **Historical** | Pre-v0.7 design rationale. The philosophy still holds; concrete names and the write pipeline have since changed. |

## Reference

| Document | What it covers |
|----------|----------------|
| [`ssh-shell-mode.md`](ssh-shell-mode.md) | The `ssh+shell://` no-deployment audit-mode variant. |
| [`changelog-old.md`](changelog-old.md) | Older changelog entries, archived from the root `CHANGELOG.md`. |
| [`feasibility/posix-support.md`](feasibility/posix-support.md) | Feasibility notes on POSIX support. |
| [`feasibility/fideduperange.md`](feasibility/fideduperange.md) | Feasibility notes on `FIDEDUPERANGE`-based deduplication. |

For tool-specific specifications, see [`cmd/dcfhfind/DESIGN.md`](../cmd/dcfhfind/DESIGN.md)
and [`cmd/dcfhfix/DESIGN.md`](../cmd/dcfhfix/DESIGN.md).
