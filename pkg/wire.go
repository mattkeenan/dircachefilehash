package dircachefilehash

import "encoding/json"

// Wire primitives for audit mode (Phase 2). These are the only request/
// response types that cross the invoker↔remote boundary. The remote holds
// no dcfh state and performs no index decisions; it only exposes read-only
// filesystem reality (ScanMetadata) and content hashes (HashFiles), plus a
// capability query (ServerInfo).
//
// Invariant: every wire payload is bounded by filter cardinality (path
// count), never by file content size. No primitive transmits raw bytes of
// audited files.
//
// FileMeta is intentionally separate from pkg.EntryInfo: EntryInfo carries
// dcfh-internal concepts (IsDeleted, HashType, wall-time encoding) that
// don't belong on the wire, and binaryEntry is a zero-copy mmap layout.
// Cross-wire types stay plain JSON-serialisable structs with no storage
// or lifecycle coupling to the index format.

// WireVersion is the wire protocol version. Incremented only on
// backwards-incompatible changes to request/response shape.
const WireVersion = 1

// WireKind identifies the request or response variant in a framed envelope.
// Stringly-typed on the wire; constants below keep call sites checkable.
type WireKind string

const (
	WireKindScanMetadata WireKind = "scan_metadata"
	WireKindHashFiles    WireKind = "hash_files"
	WireKindServerInfo   WireKind = "server_info"
	WireKindError        WireKind = "error"
)

// WireEnvelope wraps every request and response on the wire. A single
// integer ID correlates request/response pairs for out-of-order delivery
// if the transport ever supports pipelining (Phase 2 is synchronous).
//
// Payload is json.RawMessage so the inner request/response body is
// carried verbatim — not base64-encoded as a []byte would be.
type WireEnvelope struct {
	ID      uint64          `json:"id"`
	Kind    WireKind        `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WireError is returned in an envelope with Kind == WireKindError when a
// primitive fails server-side. Code is machine-readable; Message is for
// humans.
type WireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FileKind enumerates the filesystem entry types ScanMetadata distinguishes.
// Matches the subset dcfh currently indexes; unknown kinds are skipped
// server-side.
type FileKind string

const (
	FileKindRegular FileKind = "regular"
	FileKindSymlink FileKind = "symlink"
	FileKindDir     FileKind = "dir"
)

// FileMeta is one entry in a ScanMetadata response. Fields mirror what
// dcfh's binaryEntry stores (minus the hash, which is requested separately
// via HashFiles on the subset that requires it).
//
// Times are wall-clock nanoseconds since the Unix epoch, matching dcfh's
// internal wall-time encoding. Paths are relative to the scan root and
// use forward slashes regardless of remote OS.
type FileMeta struct {
	Path       string   `json:"path"`
	Kind       FileKind `json:"kind"`
	Size       int64    `json:"size"`
	Mode       uint32   `json:"mode"`
	UID        uint32   `json:"uid"`
	GID        uint32   `json:"gid"`
	MtimeNs    int64    `json:"mtime_ns"`
	CtimeNs    int64    `json:"ctime_ns"`
	Dev        uint64   `json:"dev,omitempty"`
	LinkTarget string   `json:"link_target,omitempty"`
}

// ScanRequest is the input to a ScanMetadata call. Empty Paths means
// "scan the entire root". Symlinks and Ignores mirror the client-side
// flags of the same name so the server reproduces client semantics.
type ScanRequest struct {
	Paths    []string `json:"paths,omitempty"`
	Symlinks string   `json:"symlinks,omitempty"` // matches --symlinks modes
	Ignores  []string `json:"ignores,omitempty"`  // .dcfhignore-style patterns
}

// ScanResponse carries the sorted FileMeta slice. Sort order is
// lexicographic by Path; this matches dcfh's index sort order so the
// invoker can drive Hwang–Lin directly against the stream.
//
// For large scans this slice can be unbounded (millions of entries).
// The transport commit will layer NDJSON or length-prefixed chunking
// so neither side has to buffer the whole response in memory.
type ScanResponse struct {
	Files []FileMeta `json:"files"`
}

// HashRequest asks the remote to hash the listed paths (relative to the
// scan root) with the named algorithm. Algo is one of the dcfh hash names
// ("sha1", "sha256", "sha512").
type HashRequest struct {
	Paths []string `json:"paths"`
	Algo  string   `json:"algo"`
}

// PathDigest is one entry in a HashFiles response. If Err is non-empty
// the digest is empty (file vanished, permission denied, etc.); the
// invoker decides how to handle the partial result.
type PathDigest struct {
	Path string `json:"path"`
	Hash string `json:"hash,omitempty"` // hex-encoded
	Err  string `json:"err,omitempty"`
}

// HashResponse carries the digests. Order matches HashRequest.Paths.
type HashResponse struct {
	Digests []PathDigest `json:"digests"`
}

// ServerCaps describes what the remote server supports. Returned by
// ServerInfo; the invoker uses it to negotiate wire version and select
// algorithms the remote will accept.
type ServerCaps struct {
	WireVersion int      `json:"wire_version"`
	DcfhVersion string   `json:"dcfh_version"`
	HashAlgos   []string `json:"hash_algos"`
	Concurrency int      `json:"concurrency"`
}
