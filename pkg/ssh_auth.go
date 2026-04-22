package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
)

// TOFU host-key pinning + ssh-agent passthrough for every ssh subprocess
// dcfh spawns (wire and shell transports). See docs/ssh-shell-mode.md
// for the user-facing contract.

const dcfhConfigDirName = ".config/dcfh"

// knownHostsPath returns dcfh's TOFU known_hosts file path, ensuring
// the containing directory exists at 0700. Failure indicates $HOME is
// unresolvable or the directory can't be created.
func knownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	dir := filepath.Join(home, dcfhConfigDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// sshAuthOpts returns the `-o` flags that pin ssh to dcfh's own
// known_hosts file with accept-new TOFU — unknown hosts are recorded on
// first contact, changed keys are refused. ssh-agent authentication is
// inherited via SSH_AUTH_SOCK (we don't override .Env).
func sshAuthOpts() ([]string, error) {
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	return []string{
		"-o", "UserKnownHostsFile=" + path,
		"-o", "StrictHostKeyChecking=accept-new",
	}, nil
}

// sshCommand is the full argv for spawning ssh: TOFU auth opts prepended
// to sshArgs' positional output. Shared between dialSSH (wire) and
// sshShellRunner (shell) so both transports share one auth policy.
func sshCommand(uri RepoURI, remoteCmd []string) ([]string, error) {
	auth, err := sshAuthOpts()
	if err != nil {
		return nil, fmt.Errorf("ssh auth setup: %w", err)
	}
	return append(auth, sshArgs(uri, remoteCmd)...), nil
}
