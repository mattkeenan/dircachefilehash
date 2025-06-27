package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dcfh-config-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Load config (should create default)
	config, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Check default hash algorithm
	hashConfig := config.GetHashConfig()
	if hashConfig.Default != "sha256" {
		t.Errorf("Expected default hash algorithm 'sha256', got '%s'", hashConfig.Default)
	}

	// Verify config file was created
	configPath := filepath.Join(tempDir, "config")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}
}

func TestConfigOverrides(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "dcfh-config-override-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Load config
	config, err := LoadConfig(tempDir)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Apply multiple overrides
	err = config.ApplyOverrides([]string{
		"default:sha1",
		"format:json", 
		"level:2",
		"debug:scan,extravalidation",
	})
	if err != nil {
		t.Fatalf("Failed to apply overrides: %v", err)
	}

	// Check that all overrides were applied
	allConfig := config.GetAllConfig()
	
	if allConfig.Hash.Default != "sha1" {
		t.Errorf("Expected hash algorithm 'sha1' after override, got '%s'", allConfig.Hash.Default)
	}
	
	if allConfig.Output.Format != "json" {
		t.Errorf("Expected output format 'json' after override, got '%s'", allConfig.Output.Format)
	}
	
	if allConfig.Verbose.Level != 2 {
		t.Errorf("Expected verbose level 2 after override, got %d", allConfig.Verbose.Level)
	}
	
	if allConfig.Verbose.Debug != "scan,extravalidation" {
		t.Errorf("Expected debug flags 'scan,extravalidation' after override, got '%s'", allConfig.Verbose.Debug)
	}
}

func TestHashAlgorithmValidation(t *testing.T) {
	testCases := []struct {
		algorithm string
		valid     bool
	}{
		{"sha1", true},
		{"sha256", true},
		{"sha512", true},
		{"SHA1", true},   // case insensitive
		{"SHA256", true}, // case insensitive
		{"md5", false},   // unsupported
		{"invalid", false},
		{"", false},
	}

	for _, tc := range testCases {
		err := ValidateHashAlgorithm(tc.algorithm)
		if tc.valid && err != nil {
			t.Errorf("Algorithm '%s' should be valid but got error: %v", tc.algorithm, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("Algorithm '%s' should be invalid but no error returned", tc.algorithm)
		}
	}
}

func TestGetHashAlgorithm(t *testing.T) {
	testCases := []struct {
		name     string
		typeID   uint16
		size     int
		valid    bool
	}{
		{"sha1", HashTypeSHA1, HashSizeSHA1, true},
		{"sha256", HashTypeSHA256, HashSizeSHA256, true},
		{"sha512", HashTypeSHA512, HashSizeSHA512, true},
		{"invalid", 0, 0, false},
	}

	for _, tc := range testCases {
		algo, err := GetHashAlgorithm(tc.name)
		if tc.valid {
			if err != nil {
				t.Errorf("GetHashAlgorithm('%s') should succeed but got error: %v", tc.name, err)
				continue
			}
			if algo.TypeID != tc.typeID {
				t.Errorf("GetHashAlgorithm('%s') type ID = %d, expected %d", tc.name, algo.TypeID, tc.typeID)
			}
			if algo.Size != tc.size {
				t.Errorf("GetHashAlgorithm('%s') size = %d, expected %d", tc.name, algo.Size, tc.size)
			}
		} else {
			if err == nil {
				t.Errorf("GetHashAlgorithm('%s') should fail but succeeded", tc.name)
			}
		}
	}
}

func TestConfigValidation(t *testing.T) {
	t.Run("OutputFormat", func(t *testing.T) {
		testCases := []struct {
			format string
			valid  bool
		}{
			{"human", true},
			{"json", true},
			{"fdupes", true},
			{"Human", true}, // case insensitive
			{"JSON", true},  // case insensitive
			{"FDUPES", true}, // case insensitive
			{"xml", false},
			{"", false},
		}

		for _, tc := range testCases {
			err := ValidateOutputFormat(tc.format)
			if tc.valid && err != nil {
				t.Errorf("Format '%s' should be valid but got error: %v", tc.format, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("Format '%s' should be invalid but no error returned", tc.format)
			}
		}
	})

	t.Run("VerboseLevel", func(t *testing.T) {
		testCases := []struct {
			level int
			valid bool
		}{
			{0, true},
			{1, true},
			{2, true},
			{3, true},
			{-1, false},
			{4, false},
		}

		for _, tc := range testCases {
			err := ValidateVerboseLevel(tc.level)
			if tc.valid && err != nil {
				t.Errorf("Level %d should be valid but got error: %v", tc.level, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("Level %d should be invalid but no error returned", tc.level)
			}
		}
	})
}