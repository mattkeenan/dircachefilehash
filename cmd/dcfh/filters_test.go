package main

import (
	"testing"
	"time"
)

func TestParseSizeBound(t *testing.T) {
	tests := []struct {
		in      string
		want    uint64
		wantErr bool
	}{
		{in: "0", want: 0},
		{in: "100", want: 100},
		{in: "1K", want: 1024},
		{in: "1k", want: 1024},
		{in: "2M", want: 2 * 1024 * 1024},
		{in: "1G", want: 1024 * 1024 * 1024},
		{in: "1T", want: 1024 * 1024 * 1024 * 1024},
		{in: "", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "+1", wantErr: true},
		{in: "1.5K", wantErr: true},
		{in: "abc", wantErr: true},
		{in: "1X", wantErr: true},
		{in: "K", wantErr: true},
		{in: " 1K", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSizeBound(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParsePartialDateTime_UTC(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2026", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15T06", time.Date(2026, 3, 15, 6, 0, 0, 0, time.UTC)},
		{"2026-03-15T06:30", time.Date(2026, 3, 15, 6, 30, 0, 0, time.UTC)},
		{"2026-03-15T06:30:45", time.Date(2026, 3, 15, 6, 30, 45, 0, time.UTC)},
		{"2026Z", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2026-03-15T06:30:45Z", time.Date(2026, 3, 15, 6, 30, 45, 0, time.UTC)},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parsePartialDateTime(tc.in, time.UTC)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestParsePartialDateTime_Offsets(t *testing.T) {
	tests := []struct {
		in       string
		wantUnix int64
	}{
		// 2026-01-01T00:00:00+09:00 = 2025-12-31T15:00:00Z
		{"2026-01-01T00:00:00+09:00", time.Date(2025, 12, 31, 15, 0, 0, 0, time.UTC).Unix()},
		// -05:30 offset
		{"2026-01-01T00:00:00-05:30", time.Date(2026, 1, 1, 5, 30, 0, 0, time.UTC).Unix()},
		// +0100 compact form
		{"2026-01-01T00:00:00+0100", time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC).Unix()},
		// +02 hour-only
		{"2026-01-01T00:00:00+02", time.Date(2025, 12, 31, 22, 0, 0, 0, time.UTC).Unix()},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parsePartialDateTime(tc.in, time.UTC)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Unix() != tc.wantUnix {
				t.Errorf("got unix %d (%s), want %d", got.Unix(), got, tc.wantUnix)
			}
		})
	}
}

func TestParsePartialDateTime_Invalid(t *testing.T) {
	bad := []string{
		"",
		"abc",
		"2026-13",
		"2026-00",
		"2026-01-00",
		"2026-01-32",
		"2026-02-30", // Feb never has 30 days
		"2026-01-01T24",
		"2026-01-01T00:60",
		"2026-01-01T00:00:60",
		"2026T",
		"2026-01-01+25:00",
		"202",               // too short
		"2026-1",            // month not two digits
		"2026-01-01Z+02:00", // two suffixes
	}
	for _, s := range bad {
		t.Run(s, func(t *testing.T) {
			_, err := parsePartialDateTime(s, time.UTC)
			if err == nil {
				t.Errorf("want error for %q", s)
			}
		})
	}
}

func TestParsePartialDateTime_Berlin(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("Europe/Berlin tzdata unavailable: %v", err)
	}
	// Winter: January is CET (+01:00).
	got, err := parsePartialDateTime("2026", berlin)
	if err != nil {
		t.Fatalf("parse 2026: %v", err)
	}
	_, offset := got.Zone()
	if offset != 3600 {
		t.Errorf("Jan 1 Berlin offset = %d, want 3600 (CET)", offset)
	}
	if got.Unix() != time.Date(2026, 1, 1, 0, 0, 0, 0, berlin).Unix() {
		t.Errorf("Jan 1 midnight Berlin instant mismatch")
	}

	// Summer: July is CEST (+02:00).
	got, err = parsePartialDateTime("2026-07", berlin)
	if err != nil {
		t.Fatalf("parse 2026-07: %v", err)
	}
	_, offset = got.Zone()
	if offset != 7200 {
		t.Errorf("Jul 1 Berlin offset = %d, want 7200 (CEST)", offset)
	}
}

func TestResolveZone(t *testing.T) {
	// Empty flag falls back to time.Local.
	loc, err := resolveZone("")
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if loc != time.Local {
		t.Errorf("empty flag should return time.Local, got %v", loc)
	}

	// Valid IANA.
	if _, err := resolveZone("UTC"); err != nil {
		t.Errorf("UTC: %v", err)
	}
	if loc, err := resolveZone("Europe/Berlin"); err != nil {
		t.Skipf("Europe/Berlin tzdata unavailable: %v", err)
	} else if loc.String() != "Europe/Berlin" {
		t.Errorf("got %s, want Europe/Berlin", loc)
	}

	// Bogus.
	if _, err := resolveZone("Europe/NoSuchZone"); err == nil {
		t.Errorf("bogus zone should error")
	}
}
