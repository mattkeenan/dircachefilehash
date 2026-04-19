package dircachefilehash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// WireHandler is the server-side interface that services wire requests.
// Implementations run in the remote `dcfh server --audit` process and
// hold no dcfh state — they expose read-only filesystem reality and
// content hashes only.
type WireHandler interface {
	ServerInfo(ctx context.Context) (*ServerCaps, error)
	ScanMetadata(ctx context.Context, req ScanRequest) (*ScanResponse, error)
	HashFiles(ctx context.Context, req HashRequest) (*HashResponse, error)
}

// ServeWire runs the server loop: decode envelope, dispatch to handler,
// encode response envelope. Exits on EOF from in, or on unrecoverable
// encode/decode error. Phase 2 is synchronous — one request at a time.
//
// Handler errors are converted to WireKindError envelopes carrying the
// request's ID, so the client can surface them as WireRemoteError.
// Framing or protocol errors (malformed envelope, unknown kind) cause
// ServeWire to return — the transport is no longer trustworthy.
func ServeWire(ctx context.Context, in io.Reader, out io.Writer, h WireHandler) error {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var env WireEnvelope
		if err := dec.Decode(&env); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("decode envelope: %w", err)
		}

		resp, kind, hErr := dispatchWire(ctx, h, env)
		outEnv := WireEnvelope{ID: env.ID}
		var payload []byte
		var mErr error

		if hErr != nil {
			outEnv.Kind = WireKindError
			payload, mErr = json.Marshal(WireError{Code: "server_error", Message: hErr.Error()})
		} else {
			outEnv.Kind = kind
			payload, mErr = json.Marshal(resp)
		}
		if mErr != nil {
			return fmt.Errorf("marshal %s response: %w", outEnv.Kind, mErr)
		}
		outEnv.Payload = payload

		if err := enc.Encode(outEnv); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
	}
}

// dispatchWire decodes the request payload for env.Kind and calls the
// matching handler method. The returned kind is the response kind on
// success; on handler error the caller wraps it in WireKindError.
func dispatchWire(ctx context.Context, h WireHandler, env WireEnvelope) (any, WireKind, error) {
	switch env.Kind {
	case WireKindServerInfo:
		caps, err := h.ServerInfo(ctx)
		return caps, WireKindServerInfo, err
	case WireKindScanMetadata:
		var req ScanRequest
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return nil, "", fmt.Errorf("decode scan request: %w", err)
		}
		resp, err := h.ScanMetadata(ctx, req)
		return resp, WireKindScanMetadata, err
	case WireKindHashFiles:
		var req HashRequest
		if err := json.Unmarshal(env.Payload, &req); err != nil {
			return nil, "", fmt.Errorf("decode hash request: %w", err)
		}
		resp, err := h.HashFiles(ctx, req)
		return resp, WireKindHashFiles, err
	default:
		return nil, "", fmt.Errorf("unknown wire kind: %s", env.Kind)
	}
}
