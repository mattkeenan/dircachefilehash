package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// Test helper to create mock entry
func createMockEntry(path string, size uint64, deleted bool) *dircachefilehash.EntryInfo {
	return &dircachefilehash.EntryInfo{
		Path:      path,
		IsDeleted: deleted,
		FileSize:  size,
		Mode:      0644,
		UID:       1000,
		GID:       1000,
		Dev:       123,
		MTimeWall: 0,
		CTimeWall: 0,
		HashStr:   "abc123def456",
		HashType:  1, // SHA1
	}
}

// Test helper to create eval context
func createMockContext() *EvalContext {
	return &EvalContext{
		IndexPath:    "/test/.dcfh/main.idx",
		IndexType:    "main",
		Repository:   "/test",
		EntryPath:    "test.go",
		RelativePath: "test.go",
	}
}

// Test size parsing
func TestParseSizeTest(t *testing.T) {
	tests := []struct {
		input    string
		wantSize int64
		wantMode string
		wantErr  bool
	}{
		// Basic sizes
		{"100", 100, "=", false},
		{"100c", 100, "=", false},
		{"100b", 51200, "=", false}, // 100 * 512
		{"1k", 1024, "=", false},
		{"1M", 1048576, "=", false},    // 1024 * 1024
		{"1G", 1073741824, "=", false}, // 1024 * 1024 * 1024

		// With prefixes
		{"+100", 100, "+", false},
		{"-100", 100, "-", false},
		{"+1M", 1048576, "+", false},
		{"-1k", 1024, "-", false},

		// Decimal sizes
		{"1.5k", 1536, "=", false},    // 1.5 * 1024
		{"2.5M", 2621440, "=", false}, // 2.5 * 1024 * 1024

		// Error cases
		{"", 0, "", true},
		{"+", 0, "", true},
		{"abc", 0, "", true},
		{"+abc", 0, "", true},
		{"-1.5", 1, "-", false}, // This is actually valid: mode="-", size="1.5" -> 1 byte (truncated)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			expr, err := parseSizeTest(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseSizeTest(%q) expected error, got nil", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("parseSizeTest(%q) unexpected error: %v", tt.input, err)
				return
			}

			sizeTest, ok := expr.(*SizeTest)
			if !ok {
				t.Errorf("parseSizeTest(%q) expected *SizeTest, got %T", tt.input, expr)
				return
			}

			if sizeTest.Size != tt.wantSize {
				t.Errorf("parseSizeTest(%q) size = %d, want %d", tt.input, sizeTest.Size, tt.wantSize)
			}

			if sizeTest.Mode != tt.wantMode {
				t.Errorf("parseSizeTest(%q) mode = %q, want %q", tt.input, sizeTest.Mode, tt.wantMode)
			}
		})
	}
}

// Test time parsing
func TestParseTimeTest(t *testing.T) {
	tests := []struct {
		timeSpec string
		timeType string
		wantVal  int
		wantMode string
		wantType string
		wantErr  bool
	}{
		// mtime tests
		{"7", "mtime", 7, "=", "*dircachefilehash.MTimeTest", false},
		{"+7", "mtime", 7, "+", "*dircachefilehash.MTimeTest", false},
		{"-1", "mtime", 1, "-", "*dircachefilehash.MTimeTest", false},

		// mmin tests
		{"30", "mmin", 30, "=", "*dircachefilehash.MMinTest", false},
		{"+60", "mmin", 60, "+", "*dircachefilehash.MMinTest", false},
		{"-5", "mmin", 5, "-", "*dircachefilehash.MMinTest", false},

		// ctime tests
		{"14", "ctime", 14, "=", "*dircachefilehash.CTimeTest", false},
		{"+30", "ctime", 30, "+", "*dircachefilehash.CTimeTest", false},

		// cmin tests
		{"120", "cmin", 120, "=", "*dircachefilehash.CMinTest", false},
		{"-10", "cmin", 10, "-", "*dircachefilehash.CMinTest", false},

		// Error cases
		{"", "mtime", 0, "", "", true},
		{"+", "mtime", 0, "", "", true},
		{"abc", "mtime", 0, "", "", true},
		{"7", "invalid", 0, "", "", true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_%s", tt.timeType, tt.timeSpec), func(t *testing.T) {
			expr, err := parseTimeTest(tt.timeSpec, tt.timeType)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTimeTest(%q, %q) expected error, got nil", tt.timeSpec, tt.timeType)
				}
				return
			}

			if err != nil {
				t.Errorf("parseTimeTest(%q, %q) unexpected error: %v", tt.timeSpec, tt.timeType, err)
				return
			}

			// Check type
			actualType := fmt.Sprintf("%T", expr)
			if actualType != tt.wantType {
				t.Errorf("parseTimeTest(%q, %q) type = %s, want %s", tt.timeSpec, tt.timeType, actualType, tt.wantType)
				return
			}

			// Check values based on type
			switch test := expr.(type) {
			case *MTimeTest:
				if test.Days != tt.wantVal || test.Mode != tt.wantMode {
					t.Errorf("parseTimeTest(%q, %q) MTimeTest{Days:%d, Mode:%q}, want {Days:%d, Mode:%q}",
						tt.timeSpec, tt.timeType, test.Days, test.Mode, tt.wantVal, tt.wantMode)
				}
			case *MMinTest:
				if test.Minutes != tt.wantVal || test.Mode != tt.wantMode {
					t.Errorf("parseTimeTest(%q, %q) MMinTest{Minutes:%d, Mode:%q}, want {Minutes:%d, Mode:%q}",
						tt.timeSpec, tt.timeType, test.Minutes, test.Mode, tt.wantVal, tt.wantMode)
				}
			case *CTimeTest:
				if test.Days != tt.wantVal || test.Mode != tt.wantMode {
					t.Errorf("parseTimeTest(%q, %q) CTimeTest{Days:%d, Mode:%q}, want {Days:%d, Mode:%q}",
						tt.timeSpec, tt.timeType, test.Days, test.Mode, tt.wantVal, tt.wantMode)
				}
			case *CMinTest:
				if test.Minutes != tt.wantVal || test.Mode != tt.wantMode {
					t.Errorf("parseTimeTest(%q, %q) CMinTest{Minutes:%d, Mode:%q}, want {Minutes:%d, Mode:%q}",
						tt.timeSpec, tt.timeType, test.Minutes, test.Mode, tt.wantVal, tt.wantMode)
				}
			}
		})
	}
}

// Test expression evaluation
func TestExpressionEvaluation(t *testing.T) {
	entry := createMockEntry("test.go", 1024, false)
	context := createMockContext()

	tests := []struct {
		name string
		expr Expression
		want bool
	}{
		{
			name: "NameTest match",
			expr: dircachefilehash.MustNewNameTest("*.go", true),
			want: true,
		},
		{
			name: "NameTest no match",
			expr: dircachefilehash.MustNewNameTest("*.txt", true),
			want: false,
		},
		{
			name: "PathTest match",
			expr: dircachefilehash.MustNewPathTest("test.go", true),
			want: true,
		},
		{
			name: "SizeTest equal",
			expr: &SizeTest{Size: 1024, Mode: "="},
			want: true,
		},
		{
			name: "SizeTest greater",
			expr: &SizeTest{Size: 512, Mode: "+"},
			want: true,
		},
		{
			name: "SizeTest less",
			expr: &SizeTest{Size: 2048, Mode: "-"},
			want: true,
		},
		{
			name: "EmptyTest",
			expr: &EmptyTest{},
			want: false, // entry size is 1024
		},
		{
			name: "DeletedTest",
			expr: &DeletedTest{},
			want: false, // entry not deleted
		},
		{
			name: "HashTest match",
			expr: &HashTest{Hash: "abc123def456"},
			want: true,
		},
		{
			name: "HashPrefixTest match",
			expr: &HashPrefixTest{Prefix: "abc"},
			want: true,
		},
		{
			name: "HashTypeTest SHA1",
			expr: &HashTypeTest{Type: "SHA1"},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.expr.Evaluate(entry.AsFilterEntry(), context)
			if err != nil {
				t.Errorf("Expression.Evaluate() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Expression.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test logical operators
func TestLogicalOperators(t *testing.T) {
	entry := createMockEntry("test.go", 1024, false)
	context := createMockContext()

	tests := []struct {
		name string
		expr Expression
		want bool
	}{
		{
			name: "AND both true",
			expr: &AndExpression{
				Left:  dircachefilehash.MustNewNameTest("*.go", true),
				Right: &SizeTest{Size: 1024, Mode: "="},
			},
			want: true,
		},
		{
			name: "AND one false",
			expr: &AndExpression{
				Left:  dircachefilehash.MustNewNameTest("*.go", true),
				Right: &SizeTest{Size: 2048, Mode: "="},
			},
			want: false,
		},
		{
			name: "OR one true",
			expr: &OrExpression{
				Left:  dircachefilehash.MustNewNameTest("*.txt", true),
				Right: &SizeTest{Size: 1024, Mode: "="},
			},
			want: true,
		},
		{
			name: "OR both false",
			expr: &OrExpression{
				Left:  dircachefilehash.MustNewNameTest("*.txt", true),
				Right: &SizeTest{Size: 2048, Mode: "="},
			},
			want: false,
		},
		{
			name: "NOT true becomes false",
			expr: &NotExpression{
				Expr: dircachefilehash.MustNewNameTest("*.go", true),
			},
			want: false,
		},
		{
			name: "NOT false becomes true",
			expr: &NotExpression{
				Expr: dircachefilehash.MustNewNameTest("*.txt", true),
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.expr.Evaluate(entry.AsFilterEntry(), context)
			if err != nil {
				t.Errorf("Expression.Evaluate() error = %v", err)
				return
			}
			if got != tt.want {
				t.Errorf("Expression.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test argument parsing
func TestParseArguments(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantPoints    []string
		wantExprCount int
		wantActCount  int
		wantErr       bool
	}{
		{
			name:          "basic name test",
			args:          []string{"main", "--name", "*.go"},
			wantPoints:    []string{"main"},
			wantExprCount: 1,
			wantActCount:  1, // default --print
			wantErr:       false,
		},
		{
			name:          "multiple starting points",
			args:          []string{"main", "cache", "--name", "*.go"},
			wantPoints:    []string{"main", "cache"},
			wantExprCount: 1,
			wantActCount:  1,
			wantErr:       false,
		},
		{
			name:          "default all",
			args:          []string{"--name", "*.go"},
			wantPoints:    []string{"all"},
			wantExprCount: 1,
			wantActCount:  1,
			wantErr:       false,
		},
		{
			name:          "with action",
			args:          []string{"main", "--name", "*.go", "--ls"},
			wantPoints:    []string{"main"},
			wantExprCount: 1,
			wantActCount:  1, // --ls replaces default --print
			wantErr:       false,
		},
		{
			name:          "complex expression",
			args:          []string{"main", "--name", "*.go", "--and", "--size", "+1k"},
			wantPoints:    []string{"main"},
			wantExprCount: 1, // Combined into single AND expression
			wantActCount:  1,
			wantErr:       false,
		},
		{
			name:    "missing argument",
			args:    []string{"main", "--name"},
			wantErr: true,
		},
		{
			name:    "invalid expression",
			args:    []string{"main", "--invalid"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseArguments(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseArguments() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("parseArguments() unexpected error: %v", err)
				return
			}

			if !reflect.DeepEqual(got.StartingPoints, tt.wantPoints) {
				t.Errorf("parseArguments() starting points = %v, want %v", got.StartingPoints, tt.wantPoints)
			}

			if len(got.Expressions) != tt.wantExprCount {
				t.Errorf("parseArguments() expression count = %d, want %d", len(got.Expressions), tt.wantExprCount)
			}

			if len(got.Actions) != tt.wantActCount {
				t.Errorf("parseArguments() action count = %d, want %d", len(got.Actions), tt.wantActCount)
			}
		})
	}
}

// Test starting point resolution
func TestResolveStartingPoints(t *testing.T) {
	// Create temp directory for testing
	tempDir, err := os.MkdirTemp("", "dcfhfind_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create .dcfh directory structure
	dcfhDir := filepath.Join(tempDir, ".dcfh")
	err = os.Mkdir(dcfhDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}

	// Create test index files
	testFiles := []string{"main.idx", "cache.idx", "scan-123-456.idx", "scan-789-012.idx"}
	for _, file := range testFiles {
		f, err := os.Create(filepath.Join(dcfhDir, file))
		if err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
		_ = f.Close()
	}

	tests := []struct {
		name         string
		points       []string
		wantCount    int
		wantContains []string
	}{
		{
			name:         "main only",
			points:       []string{"main"},
			wantCount:    1,
			wantContains: []string{"main.idx"},
		},
		{
			name:         "cache only",
			points:       []string{"cache"},
			wantCount:    1,
			wantContains: []string{"cache.idx"},
		},
		{
			name:         "scan all",
			points:       []string{"scan"},
			wantCount:    2,
			wantContains: []string{"scan-123-456.idx", "scan-789-012.idx"},
		},
		{
			name:         "all indices",
			points:       []string{"all"},
			wantCount:    4,
			wantContains: []string{"main.idx", "cache.idx", "scan-123-456.idx", "scan-789-012.idx"},
		},
		{
			name:         "multiple explicit",
			points:       []string{"main", "cache"},
			wantCount:    2,
			wantContains: []string{"main.idx", "cache.idx"},
		},
		{
			name:         "specific scan file",
			points:       []string{"scan-123-456"},
			wantCount:    1,
			wantContains: []string{"scan-123-456.idx"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveStartingPoints(tt.points, dcfhDir)
			if err != nil {
				t.Errorf("resolveStartingPoints() error = %v", err)
				return
			}

			if len(got) != tt.wantCount {
				t.Errorf("resolveStartingPoints() count = %d, want %d", len(got), tt.wantCount)
			}

			// Check that all expected files are present
			for _, want := range tt.wantContains {
				found := false
				for _, indexFile := range got {
					if strings.HasSuffix(indexFile.Path, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("resolveStartingPoints() missing expected file: %s", want)
				}
			}
		})
	}
}

// Test complex expression parsing
func TestComplexExpressionParsing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "simple expression",
			args:    []string{"--name", "*.go"},
			wantErr: false,
		},
		{
			name:    "AND expression",
			args:    []string{"--name", "*.go", "--and", "--size", "+1k"},
			wantErr: false,
		},
		{
			name:    "OR expression",
			args:    []string{"--name", "*.go", "--or", "--name", "*.rs"},
			wantErr: false,
		},
		{
			name:    "NOT expression",
			args:    []string{"--not", "--deleted"},
			wantErr: false,
		},
		{
			name:    "parentheses",
			args:    []string{"(", "--name", "*.go", "--or", "--name", "*.rs", ")", "--and", "--size", "+1k"},
			wantErr: false,
		},
		{
			name:    "unmatched parentheses",
			args:    []string{"(", "--name", "*.go"},
			wantErr: true,
		},
		{
			name:    "empty parentheses",
			args:    []string{"(", ")"},
			wantErr: true, // Empty parentheses should be an error
		},
		{
			name:    "multiple actions",
			args:    []string{"--name", "*.go", "--print", "--ls"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseComplexExpressions(tt.args)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseComplexExpressions() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("parseComplexExpressions() unexpected error: %v", err)
				}
			}
		})
	}
}

// Benchmark tests
func BenchmarkParseArguments(b *testing.B) {
	args := []string{"main", "cache", "--name", "*.go", "--and", "--size", "+1M", "--or", "--mtime", "-7", "--print"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parseArguments(args)
		if err != nil {
			b.Fatalf("parseArguments failed: %v", err)
		}
	}
}

func BenchmarkExpressionEvaluation(b *testing.B) {
	entry := createMockEntry("test.go", 1048576, false)
	context := createMockContext()

	expr := &AndExpression{
		Left: &OrExpression{
			Left:  dircachefilehash.MustNewNameTest("*.go", true),
			Right: dircachefilehash.MustNewNameTest("*.rs", true),
		},
		Right: &SizeTest{Size: 1000000, Mode: "+"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := expr.Evaluate(entry.AsFilterEntry(), context)
		if err != nil {
			b.Fatalf("Expression.Evaluate failed: %v", err)
		}
	}
}
