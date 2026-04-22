package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestNormaliseDupePaths(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("abs(root): %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		want    []string
		wantErr bool
	}{
		{
			name: "no args returns nil",
			args: nil,
			want: nil,
		},
		{
			name: "absolute path inside repo",
			args: []string{filepath.Join(absRoot, "sub")},
			want: []string{"sub/"},
		},
		{
			name: "trailing slash preserved after normalisation",
			args: []string{filepath.Join(absRoot, "sub") + "/"},
			want: []string{"sub/"},
		},
		{
			name: "repo root dot collapses to whole-repo fast path",
			args: []string{absRoot},
			want: nil,
		},
		{
			name:    "path outside repo rejected",
			args:    []string{"/tmp"},
			wantErr: true,
		},
		{
			name:    "parent traversal rejected",
			args:    []string{filepath.Join(absRoot, "..")},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normaliseDupePaths(absRoot, tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestYesNoFlag(t *testing.T) {
	f := yesNoFlag(true)
	if f.String() != "yes" {
		t.Errorf("default String=%q, want yes", f.String())
	}
	if err := f.Set("no"); err != nil {
		t.Fatalf("Set(no): %v", err)
	}
	if bool(f) {
		t.Errorf("after Set(no), want false")
	}
	if err := f.Set("bogus"); err == nil {
		t.Errorf("Set(bogus) should error")
	}
	if f.Type() != "yes|no" {
		t.Errorf("Type()=%q, want yes|no", f.Type())
	}
}
