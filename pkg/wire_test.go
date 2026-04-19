package dircachefilehash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

// pipeTransport is an in-process wireTransport backed by a pair of
// io.Pipes. One pipe carries client→server bytes, the other carries
// server→client bytes; together they make a full-duplex channel.
type pipeTransport struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *pipeTransport) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeTransport) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeTransport) Close() error {
	_ = p.r.Close()
	_ = p.w.Close()
	return nil
}

// newPipePair wires a WireClient and a ServeWire loop together via two
// io.Pipes. Caller gets the client transport and the server's
// (in, out) endpoints; closing the client tears everything down.
func newPipePair() (client *pipeTransport, serverIn io.ReadCloser, serverOut io.WriteCloser) {
	cr, sw := io.Pipe() // server→client
	sr, cw := io.Pipe() // client→server
	return &pipeTransport{r: cr, w: cw}, sr, sw
}

// mockHandler is a minimal WireHandler for round-trip tests.
type mockHandler struct {
	caps     *ServerCaps
	scanResp *ScanResponse
	hashResp *HashResponse
	scanErr  error
	hashErr  error
	gotScan  *ScanRequest
	gotHash  *HashRequest
}

func (m *mockHandler) ServerInfo(_ context.Context) (*ServerCaps, error) {
	return m.caps, nil
}

func (m *mockHandler) ScanMetadata(_ context.Context, req ScanRequest) (*ScanResponse, error) {
	m.gotScan = &req
	return m.scanResp, m.scanErr
}

func (m *mockHandler) HashFiles(_ context.Context, req HashRequest) (*HashResponse, error) {
	m.gotHash = &req
	return m.hashResp, m.hashErr
}

// runServer starts ServeWire in a goroutine and returns a wait func.
func runServer(t *testing.T, h WireHandler, in io.ReadCloser, out io.WriteCloser) func() error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		err := ServeWire(context.Background(), in, out, h)
		_ = in.Close()
		_ = out.Close()
		done <- err
	}()
	return func() error { return <-done }
}

func TestWireRoundTripServerInfo(t *testing.T) {
	h := &mockHandler{caps: &ServerCaps{
		WireVersion: WireVersion,
		DcfhVersion: "v0.0.0-test",
		HashAlgos:   []string{"sha1", "sha256"},
		Concurrency: 4,
	}}
	ct, si, so := newPipePair()
	wait := runServer(t, h, si, so)

	client := NewWireClient(ct)
	caps, err := client.ServerInfo(context.Background())
	if err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	if !reflect.DeepEqual(caps, h.caps) {
		t.Fatalf("caps mismatch: got %+v, want %+v", caps, h.caps)
	}
	_ = client.Close()
	if err := wait(); err != nil {
		t.Fatalf("server exited with %v", err)
	}
}

func TestWireRoundTripScanAndHash(t *testing.T) {
	h := &mockHandler{
		scanResp: &ScanResponse{Files: []FileMeta{
			{Path: "a", Kind: FileKindRegular, Size: 10, MtimeNs: 1},
			{Path: "b/c", Kind: FileKindRegular, Size: 20, MtimeNs: 2},
		}},
		hashResp: &HashResponse{Digests: []PathDigest{
			{Path: "a", Hash: "deadbeef"},
			{Path: "b/c", Err: "permission denied"},
		}},
	}
	ct, si, so := newPipePair()
	wait := runServer(t, h, si, so)

	client := NewWireClient(ct)
	ctx := context.Background()

	scanReq := ScanRequest{Paths: []string{"."}, Symlinks: "none"}
	sr, err := client.ScanMetadata(ctx, scanReq)
	if err != nil {
		t.Fatalf("ScanMetadata: %v", err)
	}
	if !reflect.DeepEqual(sr, h.scanResp) {
		t.Fatalf("scan response mismatch: got %+v", sr)
	}
	if !reflect.DeepEqual(*h.gotScan, scanReq) {
		t.Fatalf("server saw wrong scan req: %+v", h.gotScan)
	}

	hashReq := HashRequest{Paths: []string{"a", "b/c"}, Algo: "sha256"}
	hr, err := client.HashFiles(ctx, hashReq)
	if err != nil {
		t.Fatalf("HashFiles: %v", err)
	}
	if !reflect.DeepEqual(hr, h.hashResp) {
		t.Fatalf("hash response mismatch: got %+v", hr)
	}
	if !reflect.DeepEqual(*h.gotHash, hashReq) {
		t.Fatalf("server saw wrong hash req: %+v", h.gotHash)
	}

	_ = client.Close()
	if err := wait(); err != nil {
		t.Fatalf("server exited with %v", err)
	}
}

func TestWireHandlerErrorBecomesRemoteError(t *testing.T) {
	h := &mockHandler{scanErr: fmt.Errorf("disk on fire")}
	ct, si, so := newPipePair()
	wait := runServer(t, h, si, so)

	client := NewWireClient(ct)
	_, err := client.ScanMetadata(context.Background(), ScanRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	var wre *WireRemoteError
	if !errors.As(err, &wre) {
		t.Fatalf("expected *WireRemoteError, got %T: %v", err, err)
	}
	if wre.Kind != WireKindScanMetadata {
		t.Errorf("kind: got %s", wre.Kind)
	}
	if !strings.Contains(wre.Message, "disk on fire") {
		t.Errorf("message lost: %s", wre.Message)
	}
	_ = client.Close()
	_ = wait()
}

func TestWireClientDetectsRemoteClose(t *testing.T) {
	h := &mockHandler{caps: &ServerCaps{WireVersion: WireVersion}}
	ct, si, so := newPipePair()
	wait := runServer(t, h, si, so)

	client := NewWireClient(ct)
	// One successful call, then server-side EOF triggered by closing its
	// read end — the next client call must surface a clear error.
	if _, err := client.ServerInfo(context.Background()); err != nil {
		t.Fatalf("ServerInfo: %v", err)
	}
	_ = si.Close()
	_ = so.Close()
	_ = wait()

	_, err := client.ServerInfo(context.Background())
	if err == nil {
		t.Fatal("expected error after server close")
	}
	if !strings.Contains(err.Error(), "remote closed") && !strings.Contains(err.Error(), "send") {
		t.Fatalf("unexpected error text: %v", err)
	}
	_ = client.Close()
}

func TestSSHArgs(t *testing.T) {
	cases := []struct {
		name string
		uri  RepoURI
		cmd  []string
		want []string
	}{
		{
			name: "bare host",
			uri:  RepoURI{Scheme: "ssh", Host: "example.com", Path: "/srv"},
			cmd:  []string{"dcfh", "server", "--audit"},
			want: []string{"example.com", "--", "dcfh", "server", "--audit"},
		},
		{
			name: "user + port",
			uri:  RepoURI{Scheme: "ssh", User: "ops", Host: "h", Port: "2222", Path: "/srv"},
			cmd:  []string{"dcfh", "server", "--audit"},
			want: []string{"-p", "2222", "ops@h", "--", "dcfh", "server", "--audit"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sshArgs(tc.uri, tc.cmd)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDialSSHValidation(t *testing.T) {
	cases := []struct {
		name    string
		uri     RepoURI
		cmd     []string
		errText string
	}{
		{"wrong scheme", RepoURI{Scheme: "file", Path: "/x"}, []string{"dcfh"}, "ssh scheme"},
		{"missing host", RepoURI{Scheme: "ssh"}, []string{"dcfh"}, "host"},
		{"missing cmd", RepoURI{Scheme: "ssh", Host: "h"}, nil, "remote command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dialSSH(context.Background(), tc.uri, tc.cmd)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.errText) {
				t.Fatalf("error %q missing %q", err.Error(), tc.errText)
			}
		})
	}
}
