package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// flagRemoteCacheDir overrides the default on-disk hash cache path.
// Intended for tests and unusual deployments; invokers don't set this.
var flagRemoteCacheDir string

var remoteCmd = &cobra.Command{
	Use:    "remote <root>",
	Short:  "Serve an audit-mode wire session on stdin/stdout",
	Long:   remoteLongHelp,
	Hidden: true, // invoked by the invoker over ssh, not by end users
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		root := args[0]
		cachePath, err := resolveRemoteCachePath(root)
		if err != nil {
			return err
		}
		h, err := dcfh.NewRemoteHandler(root, cachePath)
		if err != nil {
			return err
		}
		defer func() { _ = h.Close() }()
		return dcfh.ServeWire(cmd.Context(), os.Stdin, os.Stdout, h)
	},
}

const remoteLongHelp = `Run as the remote-side endpoint for audit-mode ssh sessions.

The invoker's wire session spawns 'dcfh remote <root>' over ssh and speaks
the wire protocol on stdin/stdout. The remote side holds no dcfh index state —
only read-only filesystem scans (ScanMetadata) and content hashes
(HashFiles), plus a capability query (ServerInfo).

Hash caching is controlled by the invoker per-request. When requested, the
remote persists the cache in $XDG_CACHE_HOME/dcfh-remote (or ~/.cache/
dcfh-remote), keyed by path + stat + algorithm.`

func init() {
	remoteCmd.Flags().StringVar(&flagRemoteCacheDir, "cache-dir", "",
		"override the hash cache directory (default: $XDG_CACHE_HOME/dcfh-remote)")
	rootCmd.AddCommand(remoteCmd)
	dcfh.RemoteDcfhVersion = getVersionString()
}

// resolveRemoteCachePath picks the JSON file path used for the hash
// cache when the invoker requests CacheModeLocal. One file per audit
// root keeps unrelated audits from colliding in the cache.
func resolveRemoteCachePath(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base := flagRemoteCacheDir
	if base == "" {
		base, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("determine cache dir: %w", err)
		}
		base = filepath.Join(base, "dcfh-remote")
	}
	// Encode the root as a single filename component. Using a stable
	// hash keeps paths short regardless of root length.
	sum := sha1.Sum([]byte(absRoot))
	name := hex.EncodeToString(sum[:8]) + ".json"
	return filepath.Join(base, name), nil
}
