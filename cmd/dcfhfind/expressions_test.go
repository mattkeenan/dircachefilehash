package main

import (
	"testing"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// Test time-based expression creation and string representation
func TestTimeBasedExpressions(t *testing.T) {
	// Test that time-based expressions can be created and have correct string representations
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			name: "MTimeTest string",
			expr: &MTimeTest{Days: 7, Mode: "+"},
			want: "--mtime +7",
		},
		{
			name: "MMinTest string",
			expr: &MMinTest{Minutes: 30, Mode: "-"},
			want: "--mmin -30",
		},
		{
			name: "CTimeTest string",
			expr: &CTimeTest{Days: 1, Mode: "="},
			want: "--ctime =1",
		},
		{
			name: "CMinTest string",
			expr: &CMinTest{Minutes: 120, Mode: "+"},
			want: "--cmin +120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expr.String()
			if got != tt.want {
				t.Errorf("Expression.String() = %v, want %v", got, tt.want)
			}
		})
	}

	// Note: Time evaluation tests are skipped due to complex wall time conversion
	// The TimeFromWall function requires proper Go internal time format which
	// is difficult to mock correctly in tests. The parsing logic is tested
	// in TestParseTimeTest and the time expressions compile correctly.
}

// Test path matching with various patterns
func TestPathMatching(t *testing.T) {
	context := createMockContext()

	testCases := []struct {
		path     string
		pattern  string
		wantName bool
		wantPath bool
	}{
		// Name tests vs Path tests
		{"test.go", "*.go", true, true},
		{"test.rs", "*.go", false, false},
		{"main.go", "main.*", true, true},
		{"README.md", "README*", true, true},
		{"src/main.go", "*.go", true, false}, // name test checks filename only

		// Path-specific tests
		{"src/main.go", "src/*", false, true}, // name test fails, path test succeeds
		{"src/test/file.go", "src/test/*", false, true},
		{"other/file.go", "src/*", false, false},
		{"deep/nested/path/file.txt", "*/nested/*", false, false}, // glob doesn't work this way
	}

	for _, tc := range testCases {
		t.Run(tc.path+"_"+tc.pattern, func(t *testing.T) {
			entry := createMockEntry(tc.path, 1024, false)

			// Test NameTest
			nameTest := &NameTest{Pattern: tc.pattern, CaseSensitive: true}
			gotName, err := nameTest.Evaluate(entry, context)
			if err != nil {
				t.Errorf("NameTest.Evaluate() error = %v", err)
			}

			// Test PathTest
			pathTest := &PathTest{Pattern: tc.pattern, CaseSensitive: true}
			gotPath, err := pathTest.Evaluate(entry, context)
			if err != nil {
				t.Errorf("PathTest.Evaluate() error = %v", err)
			}

			// Check name test result
			if gotName != tc.wantName {
				t.Errorf("NameTest for %s with pattern %s = %v, want %v", tc.path, tc.pattern, gotName, tc.wantName)
			}

			// Check path test result
			if gotPath != tc.wantPath {
				t.Errorf("PathTest for %s with pattern %s = %v, want %v", tc.path, tc.pattern, gotPath, tc.wantPath)
			}
		})
	}
}

// Test case insensitive matching
func TestCaseInsensitiveMatching(t *testing.T) {
	context := createMockContext()
	entry := createMockEntry("Test.GO", 1024, false)

	tests := []struct {
		name string
		expr Expression
		want bool
	}{
		{
			name: "case sensitive name match fails",
			expr: &NameTest{Pattern: "*.go", CaseSensitive: true},
			want: false,
		},
		{
			name: "case insensitive name match succeeds",
			expr: &NameTest{Pattern: "*.go", CaseSensitive: false},
			want: true,
		},
		{
			name: "case sensitive path match fails",
			expr: &PathTest{Pattern: "test.go", CaseSensitive: true},
			want: false,
		},
		{
			name: "case insensitive path match succeeds",
			expr: &PathTest{Pattern: "test.go", CaseSensitive: false},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.expr.Evaluate(entry, context)
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

// Test hash-related expressions
func TestHashExpressions(t *testing.T) {
	context := createMockContext()

	tests := []struct {
		name     string
		hashStr  string
		hashType uint16
		expr     Expression
		want     bool
	}{
		{
			name:     "exact hash match",
			hashStr:  "abc123def456",
			hashType: 1,
			expr:     &HashTest{Hash: "abc123def456"},
			want:     true,
		},
		{
			name:     "exact hash case insensitive match",
			hashStr:  "ABC123DEF456",
			hashType: 1,
			expr:     &HashTest{Hash: "abc123def456"},
			want:     true,
		},
		{
			name:     "hash prefix match",
			hashStr:  "abc123def456",
			hashType: 1,
			expr:     &HashPrefixTest{Prefix: "abc"},
			want:     true,
		},
		{
			name:     "hash prefix case insensitive",
			hashStr:  "ABC123DEF456",
			hashType: 1,
			expr:     &HashPrefixTest{Prefix: "abc"},
			want:     true,
		},
		{
			name:     "hash type SHA1 match",
			hashStr:  "abc123def456",
			hashType: 1,
			expr:     &HashTypeTest{Type: "SHA1"},
			want:     true,
		},
		{
			name:     "hash type SHA256 match",
			hashStr:  "abc123def456",
			hashType: 2,
			expr:     &HashTypeTest{Type: "SHA256"},
			want:     true,
		},
		{
			name:     "hash type case insensitive",
			hashStr:  "abc123def456",
			hashType: 1,
			expr:     &HashTypeTest{Type: "sha1"},
			want:     true,
		},
		{
			name:     "hash type mismatch",
			hashStr:  "abc123def456",
			hashType: 1,
			expr:     &HashTypeTest{Type: "SHA256"},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &dircachefilehash.EntryInfo{
				Path:      "test.txt",
				IsDeleted: false,
				FileSize:  1024,
				Mode:      0644,
				UID:       1000,
				GID:       1000,
				Dev:       123,
				MTimeWall: 0,
				CTimeWall: 0,
				HashStr:   tt.hashStr,
				HashType:  tt.hashType,
			}

			got, err := tt.expr.Evaluate(entry, context)
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

// Test edge cases and error conditions
func TestExpressionEdgeCases(t *testing.T) {
	context := createMockContext()

	tests := []struct {
		name    string
		entry   *dircachefilehash.EntryInfo
		expr    Expression
		wantErr bool
	}{
		{
			name:    "empty filename pattern",
			entry:   createMockEntry("", 1024, false),
			expr:    &NameTest{Pattern: "*", CaseSensitive: true},
			wantErr: false,
		},
		{
			name:    "invalid pattern",
			entry:   createMockEntry("test.go", 1024, false),
			expr:    &NameTest{Pattern: "[", CaseSensitive: true}, // Invalid glob pattern
			wantErr: true,
		},
		{
			name:    "zero size file",
			entry:   createMockEntry("empty.txt", 0, false),
			expr:    &EmptyTest{},
			wantErr: false,
		},
		{
			name:    "deleted file",
			entry:   createMockEntry("deleted.txt", 1024, true),
			expr:    &DeletedTest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.expr.Evaluate(tt.entry, context)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expression.Evaluate() expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Expression.Evaluate() unexpected error: %v", err)
				return
			}

			// For valid cases, check specific results
			switch tt.expr.(type) {
			case *EmptyTest:
				expected := (tt.entry.FileSize == 0)
				if got != expected {
					t.Errorf("EmptyTest.Evaluate() = %v, want %v", got, expected)
				}
			case *DeletedTest:
				if got != tt.entry.IsDeleted {
					t.Errorf("DeletedTest.Evaluate() = %v, want %v", got, tt.entry.IsDeleted)
				}
			}
		})
	}
}

// Test expression string representations
func TestExpressionStrings(t *testing.T) {
	tests := []struct {
		name string
		expr Expression
		want string
	}{
		{
			name: "NameTest string",
			expr: &NameTest{Pattern: "*.go", CaseSensitive: true},
			want: "--name *.go",
		},
		{
			name: "INameTest string",
			expr: &NameTest{Pattern: "*.go", CaseSensitive: false},
			want: "--iname *.go",
		},
		{
			name: "SizeTest string",
			expr: &SizeTest{Size: 1024, Mode: "+"},
			want: "--size +1024",
		},
		{
			name: "MTimeTest string",
			expr: &MTimeTest{Days: 7, Mode: "-"},
			want: "--mtime -7",
		},
		{
			name: "AndExpression string",
			expr: &AndExpression{
				Left:  &NameTest{Pattern: "*.go", CaseSensitive: true},
				Right: &SizeTest{Size: 1024, Mode: "+"},
			},
			want: "(--name *.go --and --size +1024)",
		},
		{
			name: "NotExpression string",
			expr: &NotExpression{
				Expr: &DeletedTest{},
			},
			want: "--not --deleted",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expr.String()
			if got != tt.want {
				t.Errorf("Expression.String() = %v, want %v", got, tt.want)
			}
		})
	}
}
