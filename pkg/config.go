package dircachefilehash

import (
	"fmt"
	"os"
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
	Level int  // Default verbose level (0=quiet, 1=basic, 2=detailed, 3=trace)
	Debug string // Default debug flags (comma-separated)
}

// AllConfig represents all configuration options
type AllConfig struct {
	Hash    *HashConfig
	Output  *OutputConfig
	Verbose *VerboseConfig
}

// LoadConfig loads configuration from the .dcfh/config file
func LoadConfig(dcfhDir string) (*Config, error) {
	configPath := filepath.Join(dcfhDir, "config")
	
	cfg := &Config{
		configPath: configPath,
	}
	
	// Load existing config or create default
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config
		cfg.ini = ini.Empty()
		if err := cfg.setDefaults(); err != nil {
			return nil, fmt.Errorf("failed to set default config: %w", err)
		}
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
	} else {
		// Load existing config
		iniFile, err := ini.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
		cfg.ini = iniFile
	}
	
	return cfg, nil
}

// setDefaults sets default configuration values
func (c *Config) setDefaults() error {
	// Set default hash algorithm
	fileHashSection, err := c.ini.NewSection("filehash")
	if err != nil {
		return fmt.Errorf("failed to create filehash section: %w", err)
	}
	_, err = fileHashSection.NewKey("default", "sha256")
	if err != nil {
		return fmt.Errorf("failed to set default hash algorithm: %w", err)
	}
	
	// Set default output format
	outputSection, err := c.ini.NewSection("output")
	if err != nil {
		return fmt.Errorf("failed to create output section: %w", err)
	}
	_, err = outputSection.NewKey("format", "human")
	if err != nil {
		return fmt.Errorf("failed to set default output format: %w", err)
	}
	
	// Set default verbose settings
	verboseSection, err := c.ini.NewSection("verbose")
	if err != nil {
		return fmt.Errorf("failed to create verbose section: %w", err)
	}
	_, err = verboseSection.NewKey("level", "0")
	if err != nil {
		return fmt.Errorf("failed to set default verbose level: %w", err)
	}
	_, err = verboseSection.NewKey("debug", "")
	if err != nil {
		return fmt.Errorf("failed to set default debug flags: %w", err)
	}
	
	return nil
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

// GetAllConfig returns all configuration options
func (c *Config) GetAllConfig() *AllConfig {
	return &AllConfig{
		Hash:    c.GetHashConfig(),
		Output:  c.GetOutputConfig(),
		Verbose: c.GetVerboseConfig(),
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
		default:
			return fmt.Errorf("unsupported override key '%s' (supported: default, format, level, debug)", key)
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