package dircachefilehash

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// This file holds the dcfhfix backup-stack cluster relocated from cmd/dcfhfix
// (task 28.2, FR3). These are the pure pkg cores — stack manipulation that
// returns data/counts and never writes to stdout — so both the dcfhfix CLI
// presenters and the Repo.Fix primitive (RunFix backup-stack family) drive the
// same battle-tested logic. Human/JSON rendering stays in the CLI presenters.

// BackupMetadata records one entry on the .dcfh/fixes/<index-type>/ backup
// stack: the operation that produced it and the path it was copied from/to.
type BackupMetadata struct {
	Timestamp   time.Time `json:"timestamp"`
	Operation   string    `json:"operation"`
	Description string    `json:"description"`
	IndexFile   string    `json:"index_file"`
	BackupFile  string    `json:"backup_file"`
}

// BackupIndexType extracts the index type from the file path (e.g. "main" from
// "main.idx"); "unknown" when there is no .idx suffix.
func BackupIndexType(indexFile string) string {
	base := filepath.Base(indexFile)
	if before, ok := strings.CutSuffix(base, ".idx"); ok {
		return before
	}
	return "unknown"
}

// BackupDir returns the backup directory for indexFile's index type, walking up
// from the file to find the owning .dcfh directory.
func BackupDir(indexFile string) (string, error) {
	dir := filepath.Dir(indexFile)
	for {
		dcfhDir := filepath.Join(dir, ".dcfh")
		if info, err := os.Stat(dcfhDir); err == nil && info.IsDir() {
			return filepath.Join(dcfhDir, "fixes", BackupIndexType(indexFile)), nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find .dcfh directory")
}

// copyFile copies a file from src to dst. The single production copy helper in
// package dircachefilehash (task 28.2 resolved the prior cmd/dcfhfix vs
// recovery_test.go duplication onto this one).
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src) //nolint:gosec // G304: src/dst are the RunFix-confined subject and a path under the confineWriteDir-bounded backup stack (or the explicitly-named CLI subject); never a raw selector
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst) //nolint:gosec // G304: src/dst are the RunFix-confined subject and a path under the confineWriteDir-bounded backup stack (or the explicitly-named CLI subject); never a raw selector
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CreateBackup copies indexFile onto its backup stack with metadata, unless
// backup is false (in which case it is a no-op). When verbose>0 and not quiet a
// confirmation line is written to out (nil out suppresses it).
func CreateBackup(indexFile, operation, description string, backup bool, verbose int, quiet bool, out io.Writer) error {
	if !backup {
		return nil // backup disabled
	}

	backupDir, err := BackupDir(indexFile)
	if err != nil {
		return fmt.Errorf("failed to find backup directory: %w", err)
	}

	if err := os.MkdirAll(backupDir, 0755); err != nil { //nolint:gosec // G301: .dcfh/fixes backup dir (confined by RunFix's confineWriteDir to MetaDir for the library path), non-secret index backups
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now()
	backupFilename := fmt.Sprintf("%d-%s.idx", timestamp.Unix(), timestamp.Format("20060102T150405"))
	backupPath := filepath.Join(backupDir, backupFilename)

	if err := copyFile(indexFile, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	metadata := &BackupMetadata{
		Timestamp:   timestamp,
		Operation:   operation,
		Description: description,
		IndexFile:   indexFile,
		BackupFile:  backupPath,
	}

	metadataPath := strings.TrimSuffix(backupPath, ".idx") + ".json"
	if err := saveBackupMetadata(metadata, metadataPath); err != nil {
		// Remove the backup file if metadata save fails
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to save backup metadata: %w", err)
	}

	if verbose > 0 && !quiet && out != nil {
		fmt.Fprintf(out, "Created backup: %s\n", backupFilename)
	}

	return nil
}

// saveBackupMetadata writes backup metadata as indented JSON.
func saveBackupMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644) //nolint:gosec // G306: backup metadata sibling under the confineWriteDir-bounded .dcfh/fixes stack, non-secret (metadata + hashes)
}

// loadBackupMetadata reads backup metadata from a JSON file.
func loadBackupMetadata(path string) (*BackupMetadata, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: metadata path enumerated from the confineWriteDir-bounded .dcfh/fixes stack directory, not a caller-supplied path
	if err != nil {
		return nil, err
	}

	var metadata BackupMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// ListBackups returns all backup metadata for indexFile's stack in
// chronological order (newest first). A missing backup directory yields an
// empty slice, not an error.
func ListBackups(indexFile string) ([]*BackupMetadata, error) {
	backupDir, err := BackupDir(indexFile)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []*BackupMetadata{}, nil // no backups
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	var backups []*BackupMetadata
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			metadataPath := filepath.Join(backupDir, entry.Name())
			metadata, err := loadBackupMetadata(metadataPath)
			if err != nil {
				// Skip invalid metadata files
				continue
			}
			backups = append(backups, metadata)
		}
	}

	// Sort by timestamp, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// removeBackupFiles removes both the backup file and its metadata sibling.
func removeBackupFiles(metadata *BackupMetadata) error {
	if err := os.Remove(metadata.BackupFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove backup file: %w", err)
	}

	metadataPath := strings.TrimSuffix(metadata.BackupFile, ".idx") + ".json"
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove metadata file: %w", err)
	}

	return nil
}

// PopBackup restores the newest backup over indexFile and removes it from the
// stack, returning the restored metadata. Errors if the stack is empty.
func PopBackup(indexFile string) (*BackupMetadata, error) {
	backups, err := ListBackups(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	if len(backups) == 0 {
		return nil, fmt.Errorf("no backups available to restore")
	}

	latest := backups[0]
	if err := copyFile(latest.BackupFile, indexFile); err != nil {
		return nil, fmt.Errorf("failed to restore backup: %w", err)
	}
	if err := removeBackupFiles(latest); err != nil {
		return nil, fmt.Errorf("backup restored but failed to clean up backup files: %w", err)
	}
	return latest, nil
}

// DiscardBackup removes the newest backup from the stack without restoring it,
// returning the discarded metadata. Errors if the stack is empty.
func DiscardBackup(indexFile string) (*BackupMetadata, error) {
	backups, err := ListBackups(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}
	if len(backups) == 0 {
		return nil, fmt.Errorf("no backups available to discard")
	}

	latest := backups[0]
	if err := removeBackupFiles(latest); err != nil {
		return nil, fmt.Errorf("failed to discard backup: %w", err)
	}
	return latest, nil
}

// ClearBackups removes every backup on indexFile's stack and the (now empty)
// stack directory, returning how many were removed.
func ClearBackups(indexFile string) (int, error) {
	backups, err := ListBackups(indexFile)
	if err != nil {
		return 0, fmt.Errorf("failed to list backups: %w", err)
	}
	if len(backups) == 0 {
		return 0, nil
	}

	for _, backup := range backups {
		if err := removeBackupFiles(backup); err != nil {
			return 0, fmt.Errorf("failed to remove backup from %s: %w",
				backup.Timestamp.Format("2006-01-02 15:04:05"), err)
		}
	}

	// Remove backup directory if it's empty
	backupDir, _ := BackupDir(indexFile)
	_ = os.Remove(backupDir) // ignore error if directory not empty or doesn't exist

	return len(backups), nil
}
