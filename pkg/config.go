package dircachefilehash

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-ini/ini"
)

// Config represents the dcfh configuration
type Config struct {
	configPath string
	ini        *ini.File
}

// HashConfig represents hash algorithm configuration
type HashConfig struct {
	Default string // Default hash algorithm
}

// OutputConfig represents output format configuration
type OutputConfig struct {
	Format string // Default output format: human, json
}

// VerboseConfig represents verbosity configuration
type VerboseConfig struct {
	Level int    // Default verbose level (0=quiet, 1=basic, 2=detailed, 3=trace)
	Debug string // Default debug flags (comma-separated)
}

// SymlinkConfig represents symlink handling configuration
type SymlinkConfig struct {
	Mode string // Default symlink mode: all, internal, external, none (can append ,strict)
}

// IgnoreConfig represents ignore pattern handling configuration
type IgnoreConfig struct {
	IgnoreIsDeindex bool // Whether newly ignored files should be marked as deleted (default: true)
}

// PerformanceConfig represents performance-related configuration
type PerformanceConfig struct {
	HashWorkers      int    // Number of concurrent hash workers (default: 2)
	HashBuffer       string // Hash buffer size for interruptible hashing (default: "2M")
	IndexLockTimeout int    // Timeout in seconds for index memory locks (default: 5)
}

// RepositoryConfig represents repository location configuration.
// Only used for external repositories where the .dcfh directory is
// separate from the scanned directory.
type RepositoryConfig struct {
	Root string // Absolute path to the directory being scanned
}

// SnapshotConfig represents snapshot retention policy configuration
type SnapshotConfig struct {
	KeepHourly  int  `ini:"keep_hourly"`  // Number of hourly snapshots to keep (default: 0)
	KeepDaily   int  `ini:"keep_daily"`   // Number of daily snapshots to keep (default: 7)
	KeepWeekly  int  `ini:"keep_weekly"`  // Number of weekly snapshots to keep (default: 4)
	KeepMonthly int  `ini:"keep_monthly"` // Number of monthly snapshots to keep (default: 12)
	KeepYearly  int  `ini:"keep_yearly"`  // Number of yearly snapshots to keep (default: 3)
	DryRun      bool `ini:"dry_run"`      // Default dry-run mode (default: false)
}

// AllConfig represents all configuration options
type AllConfig struct {
	Hash        *HashConfig
	Output      *OutputConfig
	Verbose     *VerboseConfig
	Symlink     *SymlinkConfig
	Performance *PerformanceConfig
	Snapshot    *SnapshotConfig
}

// LoadConfig loads an existing configuration from the .dcfh/config file.
// Returns an error if the config file does not exist.
func LoadConfig(metaDir string) (*Config, error) {
	configPath := filepath.Join(metaDir, "config")

	iniFile, err := ini.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config file: %w", err)
	}

	return &Config{configPath: configPath, ini: iniFile}, nil
}

// CreateDefaultConfig creates a new config file with defaults and saves it to disk.
// Use this when initialising a new repository.
func CreateDefaultConfig(metaDir string) (*Config, error) {
	configPath := filepath.Join(metaDir, "config")

	cfg := &Config{
		configPath: configPath,
		ini:        ini.Empty(),
	}

	if err := cfg.setDefaults(); err != nil {
		return nil, fmt.Errorf("failed to set default config: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("failed to save default config: %w", err)
	}

	return cfg, nil
}

// defaultKeys lists every (section, key, value) seeded into a fresh
// config. Order is preserved so the on-disk layout matches what the
// old per-section code produced.
var defaultKeys = []struct{ section, key, value string }{
	{"filehash", "default", "sha256"},
	{"output", "format", "human"},
	{"verbose", "level", "0"},
	{"verbose", "debug", ""},
	{"symlink", "mode", "none"},
	{"ignore", "ignore_is_deindex", "true"},
	{"performance", "hash_workers", "2"},
	{"performance", "index_lock_timeout", "5"},
	{"snapshot", "keep_hourly", "0"},
	{"snapshot", "keep_daily", "7"},
	{"snapshot", "keep_weekly", "4"},
	{"snapshot", "keep_monthly", "12"},
	{"snapshot", "keep_yearly", "3"},
	{"snapshot", "dry_run", "false"},
}

// setDefaults sets default configuration values
func (c *Config) setDefaults() error {
	sections := map[string]*ini.Section{}
	for _, d := range defaultKeys {
		sec, ok := sections[d.section]
		if !ok {
			s, err := c.ini.NewSection(d.section)
			if err != nil {
				return fmt.Errorf("failed to create %s section: %w", d.section, err)
			}
			sections[d.section] = s
			sec = s
		}
		if _, err := sec.NewKey(d.key, d.value); err != nil {
			return fmt.Errorf("failed to set default %s.%s: %w", d.section, d.key, err)
		}
	}
	return nil
}

// newConfigForHashType builds an in-memory Config (no file backing) whose only
// meaningful setting is the default hash algorithm. dcfhfix's repo-less repair
// path uses it so a synthesised MetaStore's GetCurrentHashType() reflects the
// subject index's checksum_type without loading a .dcfh config from disk.
func newConfigForHashType(algorithmName string) *Config {
	cfg := &Config{ini: ini.Empty()}
	cfg.ini.Section("filehash").Key("default").SetValue(algorithmName)
	return cfg
}

// GetHashConfig returns the hash configuration
func (c *Config) GetHashConfig() *HashConfig {
	hashConfig := &HashConfig{
		Default: "sha256", // fallback default
	}

	if c.ini.HasSection("filehash") {
		section := c.ini.Section("filehash")
		if section.HasKey("default") {
			hashConfig.Default = section.Key("default").String()
		}
	}

	return hashConfig
}

// GetOutputConfig returns the output configuration
func (c *Config) GetOutputConfig() *OutputConfig {
	outputConfig := &OutputConfig{
		Format: "human", // fallback default
	}

	if c.ini.HasSection("output") {
		section := c.ini.Section("output")
		if section.HasKey("format") {
			outputConfig.Format = section.Key("format").String()
		}
	}

	return outputConfig
}

// GetVerboseConfig returns the verbose configuration
func (c *Config) GetVerboseConfig() *VerboseConfig {
	verboseConfig := &VerboseConfig{
		Level: 0,  // fallback default
		Debug: "", // fallback default
	}

	if c.ini.HasSection("verbose") {
		section := c.ini.Section("verbose")
		if section.HasKey("level") {
			if level, err := section.Key("level").Int(); err == nil {
				verboseConfig.Level = level
			}
		}
		if section.HasKey("debug") {
			verboseConfig.Debug = section.Key("debug").String()
		}
	}

	return verboseConfig
}

// GetSymlinkConfig returns the symlink configuration
func (c *Config) GetSymlinkConfig() *SymlinkConfig {
	symlinkConfig := &SymlinkConfig{
		Mode: "none", // fallback default
	}

	if c.ini.HasSection("symlink") {
		section := c.ini.Section("symlink")
		if section.HasKey("mode") {
			symlinkConfig.Mode = section.Key("mode").String()
		}
	}

	return symlinkConfig
}

// GetIgnoreConfig returns the ignore configuration
func (c *Config) GetIgnoreConfig() *IgnoreConfig {
	ignoreConfig := &IgnoreConfig{
		IgnoreIsDeindex: true, // fallback default
	}

	if c.ini.HasSection("ignore") {
		section := c.ini.Section("ignore")
		if section.HasKey("ignore_is_deindex") {
			if deindex, err := section.Key("ignore_is_deindex").Bool(); err == nil {
				ignoreConfig.IgnoreIsDeindex = deindex
			}
		}
	}

	return ignoreConfig
}

// GetPerformanceConfig returns the performance configuration
func (c *Config) GetPerformanceConfig() *PerformanceConfig {
	performanceConfig := &PerformanceConfig{
		HashWorkers:      2,    // fallback default
		HashBuffer:       "2M", // fallback default - 2MB buffer for interruptible hashing
		IndexLockTimeout: 5,    // fallback default - 5 seconds
	}

	if c.ini.HasSection("performance") {
		section := c.ini.Section("performance")
		if section.HasKey("hash_workers") {
			if workers, err := section.Key("hash_workers").Int(); err == nil {
				performanceConfig.HashWorkers = workers
			}
		}
		if section.HasKey("hash_buffer") {
			if bufferSize := section.Key("hash_buffer").String(); bufferSize != "" {
				performanceConfig.HashBuffer = bufferSize
			}
		}
		if section.HasKey("index_lock_timeout") {
			if timeout, err := section.Key("index_lock_timeout").Int(); err == nil {
				performanceConfig.IndexLockTimeout = timeout
			}
		}
	}

	return performanceConfig
}

// GetSnapshotConfig returns snapshot retention policy configuration
func (c *Config) GetSnapshotConfig() *SnapshotConfig {
	cfg := &SnapshotConfig{
		KeepHourly:  0,
		KeepDaily:   7,
		KeepWeekly:  4,
		KeepMonthly: 12,
		KeepYearly:  3,
		DryRun:      false,
	}

	if !c.ini.HasSection("snapshot") {
		return cfg
	}
	section := c.ini.Section("snapshot")

	intFields := []struct {
		key string
		dst *int
	}{
		{"keep_hourly", &cfg.KeepHourly},
		{"keep_daily", &cfg.KeepDaily},
		{"keep_weekly", &cfg.KeepWeekly},
		{"keep_monthly", &cfg.KeepMonthly},
		{"keep_yearly", &cfg.KeepYearly},
	}
	for _, f := range intFields {
		if section.HasKey(f.key) {
			if v, err := section.Key(f.key).Int(); err == nil {
				*f.dst = v
			}
		}
	}

	if section.HasKey("dry_run") {
		if v, err := section.Key("dry_run").Bool(); err == nil {
			cfg.DryRun = v
		}
	}

	return cfg
}

// GetRepositoryConfig returns the repository configuration.
// Returns nil Root if this is a normal (non-external) repository.
func (c *Config) GetRepositoryConfig() *RepositoryConfig {
	repoConfig := &RepositoryConfig{}

	if c.ini.HasSection("repository") {
		section := c.ini.Section("repository")
		if section.HasKey("root") {
			repoConfig.Root = section.Key("root").String()
		}
	}

	return repoConfig
}

// SetRepositoryRoot sets the root directory for an external repository.
func (c *Config) SetRepositoryRoot(root string) error {
	section, err := c.ini.NewSection("repository")
	if err != nil {
		return fmt.Errorf("failed to create repository section: %w", err)
	}
	if _, err := section.NewKey("root", root); err != nil {
		return fmt.Errorf("failed to set repository root: %w", err)
	}
	return c.Save()
}

// ResolveExternalRoot reads the [repository] root from a .dcfh directory's
// config file, returning the root path and true if found. Returns ("", false)
// if the config doesn't exist or has no repository root.
// This avoids duplicating the "load config, read root" pattern across callers.
func ResolveExternalRoot(metaDir string) (string, bool) {
	config, err := LoadConfig(metaDir)
	if err != nil {
		return "", false
	}
	repoConfig := config.GetRepositoryConfig()
	if repoConfig.Root != "" {
		return repoConfig.Root, true
	}
	return "", false
}

// GetAllConfig returns all configuration options
func (c *Config) GetAllConfig() *AllConfig {
	return &AllConfig{
		Hash:        c.GetHashConfig(),
		Output:      c.GetOutputConfig(),
		Verbose:     c.GetVerboseConfig(),
		Symlink:     c.GetSymlinkConfig(),
		Performance: c.GetPerformanceConfig(),
		Snapshot:    c.GetSnapshotConfig(),
	}
}

// SetHashDefault sets the default hash algorithm
func (c *Config) SetHashDefault(algorithm string) error {
	section := c.ini.Section("filehash")
	section.Key("default").SetValue(algorithm)
	return c.Save()
}

// SetOutputFormat sets the default output format
func (c *Config) SetOutputFormat(format string) error {
	section := c.ini.Section("output")
	section.Key("format").SetValue(format)
	return c.Save()
}

// SetVerboseLevel sets the default verbose level
func (c *Config) SetVerboseLevel(level int) error {
	section := c.ini.Section("verbose")
	section.Key("level").SetValue(fmt.Sprintf("%d", level))
	return c.Save()
}

// SetDebugFlags sets the default debug flags
func (c *Config) SetDebugFlags(debug string) error {
	section := c.ini.Section("verbose")
	section.Key("debug").SetValue(debug)
	return c.Save()
}

// SetSymlinkMode sets the default symlink mode
func (c *Config) SetSymlinkMode(mode string) error {
	section := c.ini.Section("symlink")
	section.Key("mode").SetValue(mode)
	return c.Save()
}

// SetHashWorkers sets the number of hash workers
func (c *Config) SetHashWorkers(workers int) error {
	section := c.ini.Section("performance")
	section.Key("hash_workers").SetValue(fmt.Sprintf("%d", workers))
	return c.Save()
}

// SetIndexLockTimeout sets the index lock timeout in seconds
func (c *Config) SetIndexLockTimeout(timeout int) error {
	section := c.ini.Section("performance")
	section.Key("index_lock_timeout").SetValue(fmt.Sprintf("%d", timeout))
	return c.Save()
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	return c.ini.SaveTo(c.configPath)
}

// ApplyOverrides applies command-line overrides to the configuration
// Accepts strings like "default:sha256", "format:json", "level:2", "debug:scan"
func (c *Config) ApplyOverrides(overrides []string) error {
	for _, override := range overrides {
		parts := strings.SplitN(override, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid override format '%s', expected 'key:value'", override)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "default":
			// filehash.default override
			section := c.ini.Section("filehash")
			section.Key("default").SetValue(value)
		case "format":
			// output.format override
			section := c.ini.Section("output")
			section.Key("format").SetValue(value)
		case "level":
			// verbose.level override
			section := c.ini.Section("verbose")
			section.Key("level").SetValue(value)
		case "debug":
			// verbose.debug override
			section := c.ini.Section("verbose")
			section.Key("debug").SetValue(value)
		case "mode":
			// symlink.mode override
			section := c.ini.Section("symlink")
			section.Key("mode").SetValue(value)
		case "hash_workers":
			// performance.hash_workers override
			section := c.ini.Section("performance")
			section.Key("hash_workers").SetValue(value)
		case "index_lock_timeout":
			// performance.index_lock_timeout override
			section := c.ini.Section("performance")
			section.Key("index_lock_timeout").SetValue(value)
		default:
			return fmt.Errorf("unsupported override key '%s' (supported: default, format, level, debug, mode, hash_workers, index_lock_timeout)", key)
		}
	}

	return nil
}

// ValidateHashAlgorithm validates that a hash algorithm is supported
func ValidateHashAlgorithm(algorithm string) error {
	switch strings.ToLower(algorithm) {
	case "sha1", "sha256", "sha512":
		return nil
	default:
		return fmt.Errorf("unsupported hash algorithm: %s (supported: sha1, sha256, sha512)", algorithm)
	}
}

// ValidateOutputFormat validates that an output format is supported
func ValidateOutputFormat(format string) error {
	switch strings.ToLower(format) {
	case "human", "json", "fdupes":
		return nil
	default:
		return fmt.Errorf("unsupported output format: %s (supported: human, json, fdupes)", format)
	}
}

// ValidateVerboseLevel validates that a verbose level is valid
func ValidateVerboseLevel(level int) error {
	if level < 0 || level > 3 {
		return fmt.Errorf("invalid verbose level: %d (supported: 0-3)", level)
	}
	return nil
}

// ValidateDebugFlags validates debug flags (lenient - allows any comma-separated values)
func ValidateDebugFlags(debug string) error {
	// For now, allow any debug flags - validation can be enhanced later
	return nil
}

// ValidateSymlinkMode validates that a symlink mode is supported
func ValidateSymlinkMode(mode string) error {
	// Parse mode to handle potential ,strict suffix
	parts := strings.Split(strings.ToLower(mode), ",")
	if len(parts) == 0 {
		return fmt.Errorf("empty symlink mode")
	}

	baseMode := strings.TrimSpace(parts[0])

	// Validate base mode
	switch baseMode {
	case "all", "internal", "external", "none":
		// Valid base modes
	case "contained":
		// Legacy mode, still accepted but converted to "internal"
	default:
		return fmt.Errorf("unsupported symlink mode: %s (supported: all, internal, external, none)", baseMode)
	}

	// Validate additional flags
	for i := 1; i < len(parts); i++ {
		flag := strings.TrimSpace(parts[i])
		switch flag {
		case "strict":
			// Only valid with internal or external
			if baseMode != "internal" && baseMode != "external" && baseMode != "contained" {
				return fmt.Errorf("strict flag can only be used with internal or external modes, not with %s", baseMode)
			}
		case "":
			// Ignore empty parts
		default:
			return fmt.Errorf("unsupported symlink mode flag: %s (supported: strict)", flag)
		}
	}

	return nil
}

// ValidateHashWorkers validates that the hash worker count is reasonable
func ValidateHashWorkers(workers int) error {
	if workers < 1 {
		return fmt.Errorf("hash workers must be at least 1, got: %d", workers)
	}
	if workers > 64 {
		return fmt.Errorf("hash workers should not exceed 64, got: %d", workers)
	}
	return nil
}

// ValidateIndexLockTimeout validates that the index lock timeout is reasonable
func ValidateIndexLockTimeout(timeout int) error {
	if timeout < 1 {
		return fmt.Errorf("index lock timeout must be at least 1 second, got: %d", timeout)
	}
	if timeout > 300 {
		return fmt.Errorf("index lock timeout should not exceed 300 seconds (5 minutes), got: %d", timeout)
	}
	return nil
}
