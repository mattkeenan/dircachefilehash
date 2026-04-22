package dircachefilehash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// WireDriver is the invoker-side contract for issuing wire primitives.
// The JSON-framed `*WireClient` and the shell-pipeline `*shellClient`
// both satisfy it, so wireSession / wireWalker / wireHasher are agnostic
// to which transport underlies a given ssh repo variant.
type WireDriver interface {
	ServerInfo(ctx context.Context) (*ServerCaps, error)
	ScanMetadata(ctx context.Context, req ScanRequest) (*ScanResponse, error)
	HashFiles(ctx context.Context, req HashRequest) (*HashResponse, error)
	Close() error
}

// Compile-time assertion: *WireClient is the canonical WireDriver.
var _ WireDriver = (*WireClient)(nil)

// WireClient is the invoker-side driver for the audit wire protocol. It
// owns a bidirectional transport (typically an ssh subprocess) and
// serialises calls — Phase 2 issues one request at a time. The envelope
// ID is monotonic so a future pipelined transport can correlate
// out-of-order responses without changing the wire format.
type WireClient struct {
	transport wireTransport
	enc       *json.Encoder
	dec       *json.Decoder

	mu     sync.Mutex
	nextID atomic.Uint64
	closed atomic.Bool
}

// NewWireClient wraps an already-connected transport. Ownership of the
// transport transfers; Close releases it.
func NewWireClient(t wireTransport) *WireClient {
	return &WireClient{
		transport: t,
		enc:       json.NewEncoder(t),
		dec:       json.NewDecoder(t),
	}
}

// Close releases the underlying transport. Subsequent calls error.
func (c *WireClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	return c.transport.Close()
}

// WireRemoteError is the typed form of a WireKindError response. It
// lets callers distinguish remote-reported failures (a handler returned
// an error) from transport failures (connection dropped, malformed JSON).
type WireRemoteError struct {
	Kind    WireKind
	Code    string
	Message string
}

func (e *WireRemoteError) Error() string {
	return fmt.Sprintf("remote %s error [%s]: %s", e.Kind, e.Code, e.Message)
}

// ServerInfo queries the remote's capabilities. Call once at session
// start so the invoker can verify wire-version compatibility before
// issuing real work.
func (c *WireClient) ServerInfo(ctx context.Context) (*ServerCaps, error) {
	var caps ServerCaps
	if err := c.call(ctx, WireKindServerInfo, struct{}{}, &caps); err != nil {
		return nil, err
	}
	return &caps, nil
}

// ScanMetadata asks the remote to walk its filesystem under the scan
// root and return sorted file metadata.
func (c *WireClient) ScanMetadata(ctx context.Context, req ScanRequest) (*ScanResponse, error) {
	var resp ScanResponse
	if err := c.call(ctx, WireKindScanMetadata, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// HashFiles asks the remote to compute content hashes for specific paths.
func (c *WireClient) HashFiles(ctx context.Context, req HashRequest) (*HashResponse, error) {
	var resp HashResponse
	if err := c.call(ctx, WireKindHashFiles, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// call sends one request envelope and reads one response envelope.
// Serialised on c.mu. Context cancellation closes the transport, which
// aborts any in-flight read/write; the client is unusable afterwards —
// Phase 2 doesn't support graceful mid-request recovery.
func (c *WireClient) call(ctx context.Context, kind WireKind, req, resp any) error {
	if c.closed.Load() {
		return fmt.Errorf("wire client closed")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	id := c.nextID.Add(1)
	payload, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal %s request: %w", kind, err)
	}

	// Only spawn a cancellation watcher for cancellable contexts;
	// context.Background()'s Done() returns nil, so there's nothing to
	// race against and we'd be allocating a goroutine + channel per call
	// for no benefit.
	if cancelCh := ctx.Done(); cancelCh != nil {
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-cancelCh:
				_ = c.Close()
			case <-done:
			}
		}()
	}

	if err := c.enc.Encode(WireEnvelope{ID: id, Kind: kind, Payload: payload}); err != nil {
		return fmt.Errorf("send %s: %w", kind, err)
	}

	var env WireEnvelope
	if err := c.dec.Decode(&env); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("remote closed connection while awaiting %s", kind)
		}
		return fmt.Errorf("recv %s: %w", kind, err)
	}
	if env.ID != id {
		return fmt.Errorf("wire id mismatch: sent %d, got %d", id, env.ID)
	}
	if env.Kind == WireKindError {
		var werr WireError
		if derr := json.Unmarshal(env.Payload, &werr); derr != nil {
			return fmt.Errorf("remote error (undecodable): %w", derr)
		}
		return &WireRemoteError{Kind: kind, Code: werr.Code, Message: werr.Message}
	}
	if env.Kind != kind {
		return fmt.Errorf("wire kind mismatch: sent %s, got %s", kind, env.Kind)
	}
	if resp == nil {
		return nil
	}
	if err := json.Unmarshal(env.Payload, resp); err != nil {
		return fmt.Errorf("decode %s response: %w", kind, err)
	}
	return nil
}
