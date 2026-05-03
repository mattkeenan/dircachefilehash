package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isPathContained checks if targetPath is contained within containerPath.
// Used by symlink-policy checks to decide whether a target is internal
// to the repository root.
func (dc *DirectoryCache) isPathContained(targetPath, containerPath string) bool {
	targetPath = filepath.Clean(targetPath)
	containerPath = filepath.Clean(containerPath)

	if !filepath.IsAbs(targetPath) {
		var err error
		targetPath, err = filepath.Abs(targetPath)
		if err != nil {
			return false
		}
	}

	if !filepath.IsAbs(containerPath) {
		var err error
		containerPath, err = filepath.Abs(containerPath)
		if err != nil {
			return false
		}
	}

	if targetPath == containerPath {
		return true
	}

	containerWithSep := containerPath + string(filepath.Separator)
	return strings.HasPrefix(targetPath, containerWithSep)
}

// parseSymlinkMode parses the --symlinks mode string into base mode and
// strict flag. Accepts "none|all|internal|external" optionally suffixed
// with ",strict". The legacy "contained" alias is mapped to "internal".
func parseSymlinkMode(mode string) (baseMode string, strict bool) {
	parts := strings.Split(mode, ",")
	if len(parts) == 0 {
		return "none", false
	}

	baseMode = strings.TrimSpace(parts[0])
	for i := 1; i < len(parts); i++ {
		if strings.TrimSpace(parts[i]) == "strict" {
			strict = true
		}
	}

	if baseMode == "contained" {
		baseMode = "internal"
	}

	return baseMode, strict
}

// checkSymlinkChain walks a symlink chain and reports whether all hops
// are internal (target inside RootDir) or all external. In non-strict
// mode only the final target is checked.
func (dc *DirectoryCache) checkSymlinkChain(symlinkPath string, strict bool) (allInternal, allExternal bool, err error) {
	allInternal = true
	allExternal = true

	currentPath := symlinkPath
	visited := make(map[string]bool)

	for {
		if visited[currentPath] {
			return false, false, fmt.Errorf("symlink loop detected at %s", currentPath)
		}
		visited[currentPath] = true

		info, err := os.Lstat(currentPath)
		if err != nil {
			return false, false, err
		}

		if info.Mode()&os.ModeSymlink == 0 {
			break
		}

		target, err := os.Readlink(currentPath)
		if err != nil {
			return false, false, err
		}

		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(currentPath), target)
		}

		isInternal := dc.isPathContained(target, dc.RootDir)

		if strict {
			if isInternal {
				allExternal = false
			} else {
				allInternal = false
			}

			if !allInternal && !allExternal {
				return false, false, nil
			}
		}

		currentPath = target
	}

	if !strict {
		finalTarget, err := filepath.EvalSymlinks(symlinkPath)
		if err != nil {
			return false, false, err
		}
		isInternal := dc.isPathContained(finalTarget, dc.RootDir)
		return isInternal, !isInternal, nil
	}

	return allInternal, allExternal, nil
}

// shouldFollowSymlink applies the configured --symlinks mode to a single
// symlink path. Used by shouldIndex to decide whether files reached via
// a parent symlink are still in scope.
func (dc *DirectoryCache) shouldFollowSymlink(sr *ScanRun, symlinkPath string) bool {
	baseMode, strict := parseSymlinkMode(sr.SymlinkMode)

	if IsDebugEnabled("scan") {
		VerboseLog(3, "shouldFollowSymlink: path=%s, mode=%s (base=%s, strict=%v)", symlinkPath, sr.SymlinkMode, baseMode, strict)
	}

	switch baseMode {
	case "none":
		return false
	case "all":
		return true
	case "internal":
		allInternal, _, err := dc.checkSymlinkChain(symlinkPath, strict)
		return err == nil && allInternal
	case "external":
		_, allExternal, err := dc.checkSymlinkChain(symlinkPath, strict)
		return err == nil && allExternal
	default:
		return true
	}
}

// resolveSymlinkForScan applies the symlink-mode policy to a symlink
// entry. For directory symlinks it returns the target's FileInfo so
// the scanner recurses in; for file symlinks it returns the original
// lstat info unchanged. Returns skip=true for symlinks that should be
// dropped (broken target, policy rejection, etc.).
func (dc *DirectoryCache) resolveSymlinkForScan(sr *ScanRun, currentPath string, info os.FileInfo) (os.FileInfo, bool) {
	targetInfo, err := os.Stat(currentPath)
	if err != nil {
		return nil, true
	}
	if !targetInfo.IsDir() {
		return info, false
	}
	if dc.shouldFollowDirSymlink(sr, currentPath) {
		return targetInfo, false
	}
	return nil, true
}

// shouldFollowDirSymlink applies the symlink-mode policy (none /
// internal / external / all) to a directory symlink. Debug logs
// describe every decision.
func (dc *DirectoryCache) shouldFollowDirSymlink(sr *ScanRun, currentPath string) bool {
	baseMode, strict := parseSymlinkMode(sr.SymlinkMode)
	switch baseMode {
	case "none":
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (mode=none): %s\n", currentPath)
		}
		return false
	case "internal":
		return dc.checkDirSymlinkChain(currentPath, strict, true)
	case "external":
		return dc.checkDirSymlinkChain(currentPath, strict, false)
	case "all":
		if IsDebugEnabled("symlinks") {
			finalTarget, _ := filepath.EvalSymlinks(currentPath)
			fmt.Fprintf(os.Stderr, "[SYMLINK] Following directory symlink (mode=all): %s -> %s\n", currentPath, finalTarget)
		}
		return true
	default:
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Unknown mode '%s', defaulting to 'all'\n", baseMode)
		}
		return true
	}
}

// checkDirSymlinkChain drives the internal/external symlink-chain
// check. internal=true requires all links in the chain to point
// inside dc.RootDir; internal=false requires all external.
func (dc *DirectoryCache) checkDirSymlinkChain(currentPath string, strict, internal bool) bool {
	allInternal, allExternal, err := dc.checkSymlinkChain(currentPath, strict)
	if err != nil {
		if IsDebugEnabled("symlinks") {
			fmt.Fprintf(os.Stderr, "[SYMLINK] Error checking symlink chain: %s - %v\n", currentPath, err)
		}
		return false
	}
	ok := allInternal
	label := "internal"
	if !internal {
		ok = allExternal
		label = "external"
	}
	if !ok {
		if IsDebugEnabled("symlinks") {
			finalTarget, _ := filepath.EvalSymlinks(currentPath)
			fmt.Fprintf(os.Stderr, "[SYMLINK] Skipping directory symlink (not %s): %s -> %s (root: %s, strict: %v)\n",
				label, currentPath, finalTarget, dc.RootDir, strict)
		}
		return false
	}
	if IsDebugEnabled("symlinks") {
		finalTarget, _ := filepath.EvalSymlinks(currentPath)
		fmt.Fprintf(os.Stderr, "[SYMLINK] Following %s directory symlink: %s -> %s (root: %s, strict: %v)\n",
			label, currentPath, finalTarget, dc.RootDir, strict)
	}
	return true
}
