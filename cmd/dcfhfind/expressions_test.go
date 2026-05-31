package main

import (
	"fmt"
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
		// Name tests vs Path tests (gitignore semantics: a slash-less
		// pattern matches any path component; PathTest sees all
		// segments, NameTest sees only the basename.)
		{"test.go", "*.go", true, true},
		{"test.rs", "*.go", false, false},
		{"main.go", "main.*", true, true},
		{"README.md", "README*", true, true},
		{"src/main.go", "*.go", true, true}, // gitignore *.go matches the main.go segment

		// Path-specific tests (patterns containing `/` are anchored.)
		{"src/main.go", "src/*", false, true},
		{"src/test/file.go", "src/test/*", false, true},
		{"other/file.go", "src/*", false, false},
		{"deep/nested/path/file.txt", "*/nested/*", false, true}, // gitignore prefix-match: <any>/nested/<any>/...
	}

	for _, tc := range testCases {
		t.Run(tc.path+"_"+tc.pattern, func(t *testing.T) {
			entry := createMockEntry(tc.path, 1024, false)

			// Test NameTest
			nameTest := dircachefilehash.MustNewNameTest(tc.pattern, true)
			gotName, err := nameTest.Evaluate(entry.AsFilterEntry(), context)
			if err != nil {
				t.Errorf("NameTest.Evaluate() error = %v", err)
			}

			// Test PathTest
			pathTest := dircachefilehash.MustNewPathTest(tc.pattern, true)
			gotPath, err := pathTest.Evaluate(entry.AsFilterEntry(), context)
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
			expr: dircachefilehash.MustNewNameTest("*.go", true),
			want: false,
		},
		{
			name: "case insensitive name match succeeds",
			expr: dircachefilehash.MustNewNameTest("*.go", false),
			want: true,
		},
		{
			name: "case sensitive path match fails",
			expr: dircachefilehash.MustNewPathTest("test.go", true),
			want: false,
		},
		{
			name: "case insensitive path match succeeds",
			expr: dircachefilehash.MustNewPathTest("test.go", false),
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
			expr:    dircachefilehash.MustNewNameTest("*", true),
			wantErr: false,
		},
		{
			name:    "literal-bracket pattern matches nothing",
			entry:   createMockEntry("test.go", 1024, false),
			expr:    dircachefilehash.MustNewNameTest("[", true), // gitignore: opaque, no error, no match
			wantErr: false,
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
			got, err := tt.expr.Evaluate(tt.entry.AsFilterEntry(), context)

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
			expr: dircachefilehash.MustNewNameTest("*.go", true),
			want: "--name *.go",
		},
		{
			name: "INameTest string",
			expr: dircachefilehash.MustNewNameTest("*.go", false),
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
				Left:  dircachefilehash.MustNewNameTest("*.go", true),
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

// TestParseStatefulArgTest covers the size- and date-bound tokens routed
// through parseStatefulArgTest (--min-size/--max-size/--start-date/--end-date).
// These four tokens have no other direct unit coverage, and the test pins the
// key invariant that a malformed or missing argument returns an error with
// handled=true rather than falling through to the argTestTable lookup.
func TestParseStatefulArgTest(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		arg      string // empty means no argument token supplied
		wantType string // "" when an error is expected
		wantErr  bool
	}{
		{name: "min-size valid", token: "--min-size", arg: "1M", wantType: "*dircachefilehash.MinSizeTest"},
		{name: "max-size valid", token: "--max-size", arg: "1M", wantType: "*dircachefilehash.MaxSizeTest"},
		{name: "start-date valid", token: "--start-date", arg: "2024", wantType: "*dircachefilehash.MTimeRangeTest"},
		{name: "end-date valid", token: "--end-date", arg: "2024", wantType: "*dircachefilehash.MTimeRangeTest"},
		{name: "min-size malformed", token: "--min-size", arg: "notasize", wantErr: true},
		{name: "start-date malformed", token: "--start-date", arg: "notadate", wantErr: true},
		{name: "min-size missing arg", token: "--min-size", arg: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens := []string{tt.token}
			if tt.arg != "" {
				tokens = append(tokens, tt.arg)
			}
			// pos points past the flag at its argument, matching how the
			// real parser calls parseTestToken (flag already consumed).
			p := &ExpressionParser{tokens: tokens, pos: 1, globalArgs: map[string]string{}}

			expr, handled, err := p.parseTestToken(tt.token)
			if !handled {
				t.Fatalf("parseTestToken(%q) handled = false, want true (must not fall through to argTestTable)", tt.token)
			}

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseTestToken(%q, %q) expected error, got nil", tt.token, tt.arg)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseTestToken(%q, %q) unexpected error: %v", tt.token, tt.arg, err)
			}
			if got := fmt.Sprintf("%T", expr); got != tt.wantType {
				t.Errorf("parseTestToken(%q, %q) type = %s, want %s", tt.token, tt.arg, got, tt.wantType)
			}
		})
	}
}
