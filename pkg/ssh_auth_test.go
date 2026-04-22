package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// withIsolatedHome redirects $HOME to a per-test tempdir so
// knownHostsPath() creates ~/.config/dcfh/ under a location we own and
// clean up. Returns the tempdir for assertions on filesystem state.
func withIsolatedHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestKnownHostsPathCreatesConfigDir(t *testing.T) {
	home := withIsolatedHome(t)

	path, err := knownHostsPath()
	if err != nil {
		t.Fatalf("knownHostsPath: %v", err)
	}
	wantPath := filepath.Join(home, ".config", "dcfh", "known_hosts")
	if path != wantPath {
		t.Errorf("path: got %q, want %q", path, wantPath)
	}

	info, err := os.Stat(filepath.Join(home, ".config", "dcfh"))
	if err != nil {
		t.Fatalf("config dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("~/.config/dcfh must be a directory")
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("config dir perm: got %#o, want 0o700", perm)
	}
}

func TestKnownHostsPathIdempotent(t *testing.T) {
	withIsolatedHome(t)

	p1, err := knownHostsPath()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	p2, err := knownHostsPath()
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if p1 != p2 {
		t.Errorf("paths diverge: %q vs %q", p1, p2)
	}
}

func TestSSHAuthOptsEmitsPinnedKnownHosts(t *testing.T) {
	home := withIsolatedHome(t)
	opts, err := sshAuthOpts()
	if err != nil {
		t.Fatalf("sshAuthOpts: %v", err)
	}
	want := []string{
		"-o", "UserKnownHostsFile=" + filepath.Join(home, ".config", "dcfh", "known_hosts"),
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if !reflect.DeepEqual(opts, want) {
		t.Errorf("opts:\n got %v\nwant %v", opts, want)
	}
}

func TestSSHCommandPrependsAuthOpts(t *testing.T) {
	home := withIsolatedHome(t)
	uri := RepoURI{Scheme: "ssh", User: "ops", Host: "h", Port: "2222"}
	argv, err := sshCommand(uri, []string{"dcfh", "remote", "/srv"})
	if err != nil {
		t.Fatalf("sshCommand: %v", err)
	}
	wantKH := "UserKnownHostsFile=" + filepath.Join(home, ".config", "dcfh", "known_hosts")
	if argv[0] != "-o" || argv[1] != wantKH {
		t.Errorf("first option pair: got [%q %q], want [-o %q]", argv[0], argv[1], wantKH)
	}
	if argv[2] != "-o" || argv[3] != "StrictHostKeyChecking=accept-new" {
		t.Errorf("second option pair: got [%q %q]", argv[2], argv[3])
	}
	// Positional section preserved verbatim from sshArgs.
	positional := argv[4:]
	want := []string{"-p", "2222", "ops@h", "--", "dcfh", "remote", "/srv"}
	if !reflect.DeepEqual(positional, want) {
		t.Errorf("positional argv: got %v, want %v", positional, want)
	}
}

// TestSSHCommandPropagatesHomeDirFailure proves the error path — if $HOME
// can't be resolved, sshCommand must surface it so the caller aborts
// instead of silently falling back to an unsafe ssh invocation.
func TestSSHCommandPropagatesHomeDirFailure(t *testing.T) {
	t.Setenv("HOME", "")
	_, err := sshCommand(RepoURI{Scheme: "ssh", Host: "h"}, []string{"dcfh"})
	if err == nil {
		t.Fatal("expected error when HOME is unresolvable")
	}
	if !strings.Contains(err.Error(), "home") && !strings.Contains(err.Error(), "HOME") {
		t.Errorf("error should mention home resolution: %v", err)
	}
}

// TestSSHAuthOptsDirCreationFailure proves we surface errors from
// MkdirAll when the config dir can't be created (e.g. a file already
// occupies the path).
func TestSSHAuthOptsDirCreationFailure(t *testing.T) {
	home := withIsolatedHome(t)
	// Plant a regular file at ~/.config/dcfh so MkdirAll trips on it.
	configParent := filepath.Join(home, ".config")
	if err := os.MkdirAll(configParent, 0o755); err != nil {
		t.Fatalf("prep parent: %v", err)
	}
	blocker := filepath.Join(configParent, "dcfh")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	_, err := sshAuthOpts()
	if err == nil {
		t.Fatal("expected error when config dir path is occupied by a file")
	}
	if !strings.Contains(err.Error(), "dcfh") {
		t.Errorf("error should reference the path: %v", err)
	}
}

// installFakeSSH drops a shell script named "ssh" into a fresh dir and
// prepends it to $PATH for the test. The script records its full argv
// (one per line) to a file named after the fixture's stash path and
// then exits with exitCode. Returns the stash path so the test can read
// back what the fake ssh was called with.
func installFakeSSH(t *testing.T, exitCode int) (stashPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-ssh harness assumes /bin/sh semantics")
	}
	dir := t.TempDir()
	stashPath = filepath.Join(dir, "argv.log")
	script := fmt.Sprintf(`#!/bin/sh
for a in "$@"; do
  printf '%%s\n' "$a" >> %q
done
exit %d
`, stashPath, exitCode)
	fake := filepath.Join(dir, "ssh")
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake ssh: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return stashPath
}

// readArgv returns the newline-separated argv slice the fake ssh
// recorded. Each call invocation appends to the same file; callers
// dealing with a single invocation expect one full argv here.
func readArgv(t *testing.T, stashPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(stashPath)
	if err != nil {
		t.Fatalf("read argv stash: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	return lines
}

func TestShellRunnerPassesAuthOpts(t *testing.T) {
	home := withIsolatedHome(t)
	stash := installFakeSSH(t, 0)

	uri := RepoURI{Scheme: "ssh", Transport: TransportShell, Host: "example", Path: "/srv"}
	run := sshShellRunner(uri)
	if _, err := run(context.Background(), "true"); err != nil {
		t.Fatalf("shell runner: %v", err)
	}

	got := readArgv(t, stash)
	wantKH := "UserKnownHostsFile=" + filepath.Join(home, ".config", "dcfh", "known_hosts")
	want := []string{
		"-o", wantKH,
		"-o", "StrictHostKeyChecking=accept-new",
		"example", "--",
		"true",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("fake ssh argv:\n got %v\nwant %v", got, want)
	}
}

// TestShellRunnerSurfacesHostKeyRejection simulates the TOFU-reject case
// by having the fake ssh exit non-zero — the real ssh binary exits 255
// when StrictHostKeyChecking trips on a changed key. We only need to
// prove the error crosses back through sshShellRunner.
func TestShellRunnerSurfacesHostKeyRejection(t *testing.T) {
	withIsolatedHome(t)
	installFakeSSH(t, 255)

	uri := RepoURI{Scheme: "ssh", Transport: TransportShell, Host: "example", Path: "/srv"}
	run := sshShellRunner(uri)
	_, err := run(context.Background(), "true")
	if err == nil {
		t.Fatal("expected error when ssh rejects the host key")
	}
	if !strings.Contains(err.Error(), "shell pipeline") {
		t.Errorf("error should be wrapped by shell runner: %v", err)
	}
}

// TestShellRunnerRepeatedCallsShareKnownHosts proves the second invocation
// resolves to the same known_hosts path without re-creating the config
// dir — MkdirAll is a no-op when the directory exists, so two successive
// runs against the same HOME converge on one file.
func TestShellRunnerRepeatedCallsShareKnownHosts(t *testing.T) {
	home := withIsolatedHome(t)
	stash := installFakeSSH(t, 0)

	run := sshShellRunner(RepoURI{Scheme: "ssh", Transport: TransportShell, Host: "example"})
	for i := range 2 {
		if _, err := run(context.Background(), "true"); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	argv := readArgv(t, stash)
	wantKH := "UserKnownHostsFile=" + filepath.Join(home, ".config", "dcfh", "known_hosts")
	// The stash concatenates both calls; both should begin with the
	// same auth header (7 fields: -o, KH, -o, strict, host, --, cmd).
	if len(argv) != 14 {
		t.Fatalf("want 14 argv tokens across two calls, got %d: %v", len(argv), argv)
	}
	if argv[1] != wantKH || argv[8] != wantKH {
		t.Errorf("known_hosts file diverged between calls: %q vs %q", argv[1], argv[8])
	}
}
