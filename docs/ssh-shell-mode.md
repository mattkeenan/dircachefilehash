# ssh+shell:// mode

`ssh+shell://user@host/path` is the no-deployment audit variant. Instead of
requiring `dcfh remote` on the remote host, the invoker drives the audit
through standard Unix tools over ssh — suitable for locked-down boxes where
you can't upload binaries.

## When to choose it

| scheme                         | needs remote binary | speed    | use when |
|--------------------------------|---------------------|----------|----------|
| `ssh://` (alias `ssh+wire://`) | yes (`dcfh remote`) | fastest  | default; audit with upload rights |
| `ssh+shell://`                 | no                  | slower   | locked-down host, zero deploy     |

## Remote requirements

- **GNU find** with `-printf` support. BSD/macOS find does not work.
- **coreutils** (`sha1sum`, `sha256sum`, `sha512sum`).
- A POSIX shell (`/bin/sh`); the remote login shell runs the pipeline via
  `sh -c`, so anything that supports `cd && find | sort` is sufficient.

## Invoker recommendations

Every audit primitive spawns its own ssh connection in shell mode. Without
connection reuse the audit pays ssh handshake cost (50–500 ms) *per file* in
the hashing phase. Enable OpenSSH connection sharing in `~/.ssh/config`:

```
Host *
    ControlMaster auto
    ControlPath   ~/.ssh/cm-%r@%h:%p
    ControlPersist 60s
```

After the first connection, subsequent ssh invocations reuse the multiplexed
channel and complete in ~5 ms. If you already use this pattern for git,
nothing else is needed.

## Limitations

- **Paths containing tab or newline characters are not representable.** The
  wire format is tab-separated, newline-delimited; records with embedded
  separators would corrupt parsing. This is a known shell-mode-only
  constraint — use `ssh://` (wire mode) if you need to audit such paths.
- **No hash cache.** The shell variant is inherently stateless on the remote
  side (each RPC is a fresh ssh invocation). For repeated scans of a large
  tree, prefer `ssh://` with `--remote-cache` if that's available.
- **Ignores are applied invoker-side.** All `.dcfhignore` filtering happens
  after the remote returns its listing, costing bandwidth on very-wide
  repositories with many ignored entries.

## Host-key TOFU

dcfh keeps its ssh host fingerprints in `~/.config/dcfh/known_hosts`,
separate from `~/.ssh/known_hosts`, so interactive ssh and automated
audit runs cannot poison each other. First contact records the host's
key; a later run against the same host whose key has changed fails
loudly (ssh exits 255, dcfh surfaces the error). This applies to both
`ssh://` and `ssh+shell://`.

Authentication is delegated to `ssh` and `ssh-agent` — dcfh inherits
`SSH_AUTH_SOCK` and never reads key material itself. Configure the
remote's authorised keys via your normal ssh workflow.

## Verifying

Check the remote meets the shell-mode requirements before your first audit:

```sh
ssh host 'find --version | head -1; sha256sum --version | head -1'
```

Both lines should start with `find (GNU findutils)` / `sha256sum (GNU
coreutils)`.
