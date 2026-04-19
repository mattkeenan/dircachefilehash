package dircachefilehash

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// wireTransport is the bidirectional byte stream underlying a WireClient.
// Reader delivers server→client envelopes; Writer delivers client→server
// envelopes; Close tears down the session. Implementations: sshTransport
// (production), plus an in-process pipe pair used by tests.
type wireTransport interface {
	io.Reader
	io.Writer
	io.Closer
}

// sshTransport drives a remote `dcfh server --audit` over ssh. It wraps
// an exec.Cmd whose stdin/stdout carry the wire protocol; stderr is
// forwarded to the invoker's stderr so ssh diagnostics (auth prompts,
// host-key warnings, remote panics) stay visible.
type sshTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

// dialSSH spawns an ssh subprocess targeting uri and runs remoteCmd on
// the far side. It does not verify the remote speaks the wire protocol;
// the first Call handshakes via ServerInfo.
func dialSSH(ctx context.Context, uri RepoURI, remoteCmd []string) (*sshTransport, error) {
	if uri.Scheme != "ssh" {
		return nil, fmt.Errorf("dialSSH requires ssh scheme, got %q", uri.Scheme)
	}
	if uri.Host == "" {
		return nil, fmt.Errorf("dialSSH requires a host")
	}
	if len(remoteCmd) == 0 {
		return nil, fmt.Errorf("dialSSH requires a remote command")
	}

	cmd := exec.CommandContext(ctx, "ssh", sshArgs(uri, remoteCmd)...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("ssh stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("ssh stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("ssh start: %w", err)
	}
	return &sshTransport{cmd: cmd, stdin: stdin, stdout: stdout}, nil
}

// sshArgs builds the argv for the ssh subprocess. Exposed (package-
// private) so tests can assert the invocation without spawning ssh.
func sshArgs(uri RepoURI, remoteCmd []string) []string {
	var args []string
	if uri.Port != "" {
		args = append(args, "-p", uri.Port)
	}
	target := uri.Host
	if uri.User != "" {
		target = uri.User + "@" + uri.Host
	}
	args = append(args, target, "--")
	return append(args, remoteCmd...)
}

func (t *sshTransport) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *sshTransport) Write(p []byte) (int, error) { return t.stdin.Write(p) }

// Close shuts the transport down. Closing stdin signals EOF to the
// remote server loop so it exits cleanly; Wait reaps the process. ssh
// commonly exits non-zero on connection teardown we initiated, so the
// Wait error is intentionally swallowed.
func (t *sshTransport) Close() error {
	_ = t.stdin.Close()
	_ = t.stdout.Close()
	_ = t.cmd.Wait()
	return nil
}
