# dircachefilehash

A fast directory-scanning, file-hashing, and duplicate-detection tool. `dcfh`
maintains a compact, git-inspired binary index (SHA-1 content hashes plus full
Unix metadata) so that integrity checks, change detection, and duplicate
identification stay quick even on trees of tens of millions of files.

## The tools

`dircachefilehash` ships three command-line programs:

- **`dcfh`** — daily operations: initialise a repository, check status, update
  the index, find duplicates, manage snapshots and configuration.
- **`dcfhfind`** — a find(1)-style search tool for index files, with pattern,
  size, time, hash, and validity predicates and AND/OR/NOT expressions.
- **`dcfhfix`** — a repair tool for damaged index files (header, entry, and
  full-scan repair, with dry-run and backup support).

## Installation

### From a release package (Linux)

Releases are built for Linux (amd64 and arm64) as `.deb`, `.rpm`, and `.tar.gz`
artefacts:

- **Debian/Ubuntu**: `sudo dpkg -i dcfh_*.deb`
- **Fedora/RHEL**: `sudo rpm -i dcfh_*.rpm`
- **Other**: extract the `.tar.gz` and place the binaries on your `PATH`.

### From source

Requires Go 1.25 or later on a Unix-like system (the tool uses `syscall.Stat_t`
and mmap, so it is Linux/Unix-only).

```bash
make build      # produces ./dcfh, ./dcfhfind, ./dcfhfix
./dcfh --help
```

## Quick start

```bash
# Initialise a repository for a directory
dcfh init /path/to/dir

# See what has changed (status hashes changed files and caches the results)
dcfh status

# Record the current state of the tree in the index
dcfh update

# Find duplicate files by content hash
dcfh dupes
```

`status` is not read-only: it hashes files whose metadata changed and persists
those hashes to the cache index, so subsequent operations stay fast.

## `dcfh` commands

| Command | Purpose |
|---------|---------|
| `init <directory>` | Initialise a new dcfh repository. |
| `status` | Show the status of files in the repository. |
| `update [paths...]` | Update the index with current file states. |
| `dupes [paths...]` | Find and display duplicate files. |
| `snapshot <subcommand>` | Create and manage index-state snapshots (`create`, `list`, `forget`, `remove`, `status`). |
| `config [key] [value]` | Get and set repository configuration options. |
| `diff <left-ref> <right-ref>` | Compare any two index references. |
| `subrepo <subcommand>` | Discover and manage nested repositories (`find`, `add`). |
| `completion [bash\|zsh]` | Generate a shell completion script. |
| `version` | Show version information. |

Every command has detailed help: `dcfh <command> help` (or `--help`).

### Interactive tree viewer

`dcfh status` and `dcfh update` accept `--interactive-tree`, which opens a
gdu-style full-screen tree of the result after the run. It requires an
interactive terminal (TTY).

## `dcfhfind`

A Unix `find`-style interface for searching index files:

```bash
dcfhfind main --name "*.go" --print
dcfhfind all --corrupt --validate
dcfhfind cache --size +100M --ls
```

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and
[`cmd/dcfhfind/DESIGN.md`](cmd/dcfhfind/DESIGN.md) for the full predicate and
action surface.

## `dcfhfix`

Targeted repair for corrupted index files:

```bash
dcfhfix .dcfh/main.idx header --dry-run
dcfhfix .dcfh/cache.idx entry --offset 1024
dcfhfix .dcfh/scan-123.idx scan --backup
```

See [`cmd/dcfhfix/DESIGN.md`](cmd/dcfhfix/DESIGN.md) for the repair workflows.

## Global options

These persistent flags apply across `dcfh` commands:

| Flag | Meaning |
|------|---------|
| `-o, --output` | Output format: `human` (default), `json`, `fdupes`. |
| `-j, --json` | Shorthand for `--output=json`. |
| `-v, --verbose` | Increase verbosity (repeat: `-v`, `-vv`, `-vvv`). |
| `--symlinks` | Directory-symlink handling: `none` (default), `all`, `internal`, `external`. |
| `-s, --follow-symlinks` | Alias for `--symlinks=all`. |
| `-w, --hash-workers` | Number of concurrent hash workers (`0` = config default). |
| `-f, --filehash` | Hash-algorithm override (e.g. `default:sha256`). |
| `--dry-run` | Show what would be done without doing it. |
| `--meta-dir` | Use an external `.dcfh` directory instead of auto-discovery. |

## Documentation

Architecture and design documentation lives under [`docs/`](docs/):

- [`docs/README.md`](docs/README.md) — index of all documentation, with a
  current/historical marker for each document.
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — the current architecture
  overview: layers, index file types, and the core system metaphors.

## Contributing

Contributions are welcome — pull requests, bug reports, and feature suggestions
alike. Please ensure `go build ./...` and `go test ./...` pass before
submitting.

## License

MIT License — see the [LICENSE](LICENSE) file for details.
