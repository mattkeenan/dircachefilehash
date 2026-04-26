package dircachefilehash

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ErrRemoteNotImplemented is returned when a URI scheme requires a
// remote transport that is not yet available (Phase 2/3).
var ErrRemoteNotImplemented = errors.New("remote dcfh repositories are not yet supported (Phase 2)")

// Options carries command-level settings across the Repo boundary.
// It replaces the stringly-typed map[string]string flags dict at the
// invoker↔repo seam; wire-serialisable for remote transports.
type Options struct {
	Verbose          int    `json:"verbose,omitempty"`
	Filehash         string `json:"filehash,omitempty"`
	Symlinks         string `json:"symlinks,omitempty"`
	HashWorkers      int    `json:"hash_workers,omitempty"`
	IndexLockTimeout int    `json:"index_lock_timeout,omitempty"`
}

// toFlags converts Options to the internal map[string]string used by
// ApplyConfigOverrides. Empty/zero fields are omitted so they fall back
// to config defaults.
func (o Options) toFlags() map[string]string {
	flags := make(map[string]string)
	if o.Verbose > 0 {
		flags["v"] = fmt.Sprintf("%d", o.Verbose)
	}
	if o.Filehash != "" {
		flags["filehash"] = o.Filehash
	}
	if o.Symlinks != "" {
		flags["symlinks"] = o.Symlinks
	}
	if o.HashWorkers > 0 {
		flags["hash_workers"] = fmt.Sprintf("%d", o.HashWorkers)
	}
	if o.IndexLockTimeout > 0 {
		flags["index_lock_timeout"] = fmt.Sprintf("%d", o.IndexLockTimeout)
	}
	return flags
}

// RepoInfo is the header-level information every CLI command prints.
type RepoInfo struct {
	RootDir        string    `json:"root_dir"`
	MetaDir        string    `json:"meta_dir"`
	EntryCount     int       `json:"entry_count"`
	IndexFile      string    `json:"index_file"`
	IndexTimestamp time.Time `json:"index_timestamp"`
}

// RepoStats reports aggregate counts and total size for the active index.
// Named distinctly from the existing DirectoryCache.Stats method to avoid
// collision in wrapper code.
type RepoStats struct {
	FileCount int   `json:"file_count"`
	TotalSize int64 `json:"total_size"`
}

// DiffRequest selects what Diff should compare against the filesystem.
// Empty Paths means "entire repository".
//
// Diff is a structured delta, not a listing — it reports which entries
// were added, modified, or deleted relative to the baseline index. The
// CLI surface is still `dcfh status`; only the internal primitive is
// named Diff to reflect the return shape.
type DiffRequest struct {
	Options Options  `json:"options"`
	Paths   []string `json:"paths,omitempty"`
}

// DiffRefsRequest drives the generalised Diff engine, comparing two index
// references identified by selector strings (see ResolveIndexSelectors for
// the vocabulary). Empty Left defaults to "main"; empty Right defaults to
// "fs-scan", giving the historical dcfh-status semantics by default.
type DiffRefsRequest struct {
	Options Options `json:"options"`
	Left    string  `json:"left,omitempty"`
	Right   string  `json:"right,omitempty"`
}

// ApplyRequest is the analogue of DiffRequest for Apply (dcfh update).
type ApplyRequest struct {
	Options Options  `json:"options"`
	Paths   []string `json:"paths,omitempty"`
}

// GroupsRequest selects duplicate-detection options.
//
// Filter narrows which index entries participate — paths, size bounds,
// mtime bounds. The zero DupeFilter is the whole-repo fast path. See
// DupeFilter for the precise semantics of each field.
type GroupsRequest struct {
	Options Options    `json:"options"`
	Filter  DupeFilter `json:"filter,omitzero"`
}

// UpdateResult summarises what Apply did.
type UpdateResult struct {
	FileCount    int      `json:"file_count"`
	TotalSize    int64    `json:"total_size"`
	PathsUpdated []string `json:"paths_updated,omitempty"`
}

// Repo is the transport-neutral surface that every CLI command uses.
// Implementations: localRepo wrapping a DirectoryCache; the DirectoryCache's
// walker/hasher pair is swapped to the wire-backed pair for ssh:// roots.
// colocatedRepo (Phase 3) will proxy the full interface. Fix is deferred
// to Phase 1b.
//
// Session semantics: Repo owns index handles / RPC sessions. Close releases
// them. Snapshots() and Config() return views into the same session —
// they do not need independent Close calls.
type Repo interface {
	Close() error

	Info(ctx context.Context) (*RepoInfo, error)
	Stats(ctx context.Context) (*RepoStats, error)

	Diff(ctx context.Context, req DiffRequest) (*StatusResult, error)
	DiffRefs(ctx context.Context, req DiffRefsRequest) (*StatusResult, error)
	Apply(ctx context.Context, req ApplyRequest) (*UpdateResult, error)
	Groups(ctx context.Context, req GroupsRequest) ([]DuplicateGroup, error)
	Filter(ctx context.Context, req FilterRequest) (*FilterResult, error)

	Snapshots() SnapshotRepo
	Config() ConfigRepo
}

// SnapshotRepo exposes snapshot primitives on an open Repo.
type SnapshotRepo interface {
	Create(ctx context.Context, tags []string) (*SnapshotMetadata, error)
	List(ctx context.Context) ([]*SnapshotMetadata, error)
	Prune(ctx context.Context, policy RetentionPolicy, dryRun bool) ([]string, error)
	Delete(ctx context.Context, id string) error
}

// ConfigRepo exposes configuration primitives on an open Repo.
type ConfigRepo interface {
	Get(ctx context.Context) (*AllConfig, error)
	Set(ctx context.Context, key, value string) error
}

// RepoURI is the parsed form of a repository specifier. For Phase 1 only
// file:// (implicit or explicit) is supported; ssh:// parses but returns
// ErrRemoteNotImplemented from the factories.
type RepoURI struct {
	Scheme    string // "file" or "ssh"
	Transport string // ssh only: "wire" (default) or "shell"
	User      string // ssh only
	Host      string // ssh only
	Port      string // ssh only
	Path      string // filesystem path (absolute for file, server path for ssh)
}

// Transport constants for RepoURI.Transport (ssh scheme only).
const (
	TransportWire  = "wire"  // framed JSON-RPC to `dcfh remote` (default)
	TransportShell = "shell" // shell pipeline (find -printf + sha256sum)
)

// String renders a RepoURI as its canonical string form — symmetric with
// ParseRepoURI, so round-trips preserve all components. Bare ssh:// is
// emitted for Transport=="wire" (the default) and ssh+shell:// for shell.
func (u RepoURI) String() string {
	switch u.Scheme {
	case "ssh":
		s := "ssh"
		if u.Transport == TransportShell {
			s += "+shell"
		}
		s += "://"
		if u.User != "" {
			s += u.User + "@"
		}
		s += u.Host
		if u.Port != "" {
			s += ":" + u.Port
		}
		s += u.Path
		return s
	case "file":
		return "file://" + u.Path
	default:
		return u.Path
	}
}

// IsRemote reports whether s references a remote repository (scheme
// other than "file"). The check is cheap and avoids parsing for the
// common dispatch decision.
func IsRemote(s string) bool {
	return strings.Contains(s, "://") && !strings.HasPrefix(s, "file://")
}

// ParseRepoURI parses a string into a RepoURI. Accepts:
//   - bare absolute path (/abs/foo.dcfh) → file scheme
//   - bare relative path (./rel) → file scheme, resolved to absolute
//   - file:///abs/path → file scheme
//   - ssh://[user@]host[:port]/path → ssh scheme, wire transport (default)
//   - ssh+wire://[user@]host[:port]/path → ssh scheme, wire transport (explicit)
//   - ssh+shell://[user@]host[:port]/path → ssh scheme, shell transport
//
// Phase 1 rejects ssh:// on --meta-dir via the factory, not here; parsing
// succeeds so error messages can reference the parsed components.
func ParseRepoURI(s string) (RepoURI, error) {
	if s == "" {
		return RepoURI{}, fmt.Errorf("empty repository URI")
	}

	if transport, rest, ok := cutSSHScheme(s); ok {
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return RepoURI{}, fmt.Errorf("ssh URI missing path: %s", s)
		}
		authority := rest[:slash]
		path := rest[slash:]

		var user, hostport string
		if before, after, ok := strings.Cut(authority, "@"); ok {
			user = before
			hostport = after
		} else {
			hostport = authority
		}

		var host, port string
		if colon := strings.LastIndex(hostport, ":"); colon >= 0 {
			host = hostport[:colon]
			port = hostport[colon+1:]
		} else {
			host = hostport
		}
		if host == "" {
			return RepoURI{}, fmt.Errorf("ssh URI missing host: %s", s)
		}
		return RepoURI{Scheme: "ssh", Transport: transport, User: user, Host: host, Port: port, Path: path}, nil
	}

	if after, ok := strings.CutPrefix(s, "file://"); ok {
		path := after
		if !filepath.IsAbs(path) {
			return RepoURI{}, fmt.Errorf("file:// URI requires an absolute path: %s", s)
		}
		return RepoURI{Scheme: "file", Path: path}, nil
	}

	if scheme, _, ok := strings.Cut(s, "://"); ok {
		return RepoURI{}, fmt.Errorf("unsupported URI scheme %q (supported: file, ssh, ssh+wire, ssh+shell)", scheme)
	}

	abs, err := filepath.Abs(s)
	if err != nil {
		return RepoURI{}, fmt.Errorf("failed to resolve path %q: %w", s, err)
	}
	return RepoURI{Scheme: "file", Path: abs}, nil
}

// cutSSHScheme strips a recognised ssh scheme prefix from s and returns
// the resolved transport plus the remainder. Bare ssh:// is shorthand
// for ssh+wire:// (the default). Unknown ssh+foo:// schemes return ok=false
// so the caller can surface them via the unsupported-scheme error path.
func cutSSHScheme(s string) (transport, rest string, ok bool) {
	if after, hit := strings.CutPrefix(s, "ssh+wire://"); hit {
		return TransportWire, after, true
	}
	if after, hit := strings.CutPrefix(s, "ssh+shell://"); hit {
		return TransportShell, after, true
	}
	if after, hit := strings.CutPrefix(s, "ssh://"); hit {
		return TransportWire, after, true
	}
	return "", "", false
}

// OpenRepo opens an existing repository. In Phase 1 only file:// is
// supported; ssh:// returns ErrRemoteNotImplemented.
//
// metaDirSpec is either a path or a URI identifying the metadata directory
// (the .dcfh or *.dcfh directory). The factory resolves rootDir via
// ResolveRepository / DiscoverRepository as appropriate.
func OpenRepo(ctx context.Context, metaDirSpec string) (Repo, error) {
	uri, err := ParseRepoURI(metaDirSpec)
	if err != nil {
		return nil, err
	}
	switch uri.Scheme {
	case "file":
		return openRepoFromMetaDir(ctx, uri.Path)
	case "ssh":
		return nil, fmt.Errorf("--meta-dir does not accept ssh:// (put the URI in [repository] root instead): %s", metaDirSpec)
	default:
		return nil, fmt.Errorf("unsupported scheme %q", uri.Scheme)
	}
}

// CreateRepo creates a new repository on disk and returns an open Repo
// handle for it. Used by `dcfh init`. metaDirSpec may be empty (meaning
// ".dcfh" under rootDir) or point to an external *.dcfh directory.
//
// rootDir may be an ssh:// URI to create an audit repository: the .dcfh
// lives locally (metaDirSpec is required in this case) and the remote
// URI is persisted in [repository] root. Subsequent Diff/Apply calls
// drive the audit protocol against that host.
func CreateRepo(ctx context.Context, rootDir, metaDirSpec string) (Repo, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("CreateRepo requires a root directory")
	}

	if IsRemote(rootDir) {
		rootURI, err := ParseRepoURI(rootDir)
		if err != nil {
			return nil, fmt.Errorf("invalid remote root %q: %w", rootDir, err)
		}
		if rootURI.Scheme != "ssh" {
			return nil, fmt.Errorf("unsupported remote scheme %q (only ssh is supported)", rootURI.Scheme)
		}
		if metaDirSpec == "" {
			return nil, fmt.Errorf("audit repositories require --meta-dir (the local .dcfh directory)")
		}
		metaURI, err := ParseRepoURI(metaDirSpec)
		if err != nil {
			return nil, err
		}
		if metaURI.Scheme != "file" {
			return nil, fmt.Errorf("--meta-dir must be a local path for audit repositories")
		}
		return createWireRepo(ctx, metaURI.Path, rootURI)
	}

	if metaDirSpec == "" {
		return createLocalRepo(ctx, rootDir, "")
	}

	uri, err := ParseRepoURI(metaDirSpec)
	if err != nil {
		return nil, err
	}
	switch uri.Scheme {
	case "file":
		return createLocalRepo(ctx, rootDir, uri.Path)
	case "ssh":
		return nil, fmt.Errorf("--meta-dir does not accept ssh:// (put the URI in rootDir instead): %s", metaDirSpec)
	default:
		return nil, fmt.Errorf("unsupported scheme %q", uri.Scheme)
	}
}
