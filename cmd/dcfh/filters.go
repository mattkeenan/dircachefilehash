package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

// parseSizeBound parses N[K|M|G|T] into bytes (binary units). The
// surface is intentionally narrower than dcfhfind's parseSizeTest —
// no signs, no floats, no c/w/b — so `--min-size`/`--max-size` have
// one unambiguous meaning.
func parseSizeBound(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("size bound is empty")
	}
	digits := s
	mult := uint64(1)
	switch last := s[len(s)-1]; last {
	case 'K', 'k':
		digits, mult = s[:len(s)-1], 1024
	case 'M', 'm':
		digits, mult = s[:len(s)-1], 1024*1024
	case 'G', 'g':
		digits, mult = s[:len(s)-1], 1024*1024*1024
	case 'T', 't':
		digits, mult = s[:len(s)-1], 1024*1024*1024*1024
	default:
		if last < '0' || last > '9' {
			return 0, fmt.Errorf("invalid size suffix %q; want K, M, G, or T", string(last))
		}
	}
	if digits == "" {
		return 0, fmt.Errorf("size bound %q has no digits", s)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	if mult > 1 && n > math.MaxUint64/mult {
		return 0, fmt.Errorf("size %q overflows uint64", s)
	}
	return n * mult, nil
}

// Matches: ^(YYYY)(-MM)?(-DD)?(THH)?(:MM)?(:SS)?(Z|±hh(:mm)?)?$
// HH/MM/SS are tied to the T separator so "2026-01-01:30" can't slip
// through as "2026 at ?:30".
var partialDateTimeRE = regexp.MustCompile(
	`^(\d{4})(?:-(\d{2}))?(?:-(\d{2}))?(?:T(\d{2})(?::(\d{2}))?(?::(\d{2}))?)?(Z|[+-]\d{2}(?::?\d{2})?)?$`,
)

// parsePartialDateTime parses a partial ISO-8601 date-time. Missing
// fields default to the first instant at the given precision, so a
// bare year as --end-date excludes Jan 1 and everything after. An
// explicit Z/±offset overrides zone for that instant only.
func parsePartialDateTime(s string, zone *time.Location) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("date is empty")
	}
	if zone == nil {
		zone = time.UTC
	}
	m := partialDateTimeRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, fmt.Errorf("invalid date %q; want YYYY[-MM[-DD[THH[:MM[:SS]]]]][Z|±hh[:mm]]", s)
	}

	field := func(idx, defaultVal, min, max int, name string) (int, error) {
		if m[idx] == "" {
			return defaultVal, nil
		}
		v, _ := strconv.Atoi(m[idx])
		if v < min || v > max {
			return 0, fmt.Errorf("invalid %s in %q", name, s)
		}
		return v, nil
	}

	year, _ := strconv.Atoi(m[1])
	month, err := field(2, 1, 1, 12, "month")
	if err != nil {
		return time.Time{}, err
	}
	day, err := field(3, 1, 1, 31, "day")
	if err != nil {
		return time.Time{}, err
	}
	hour, err := field(4, 0, 0, 23, "hour")
	if err != nil {
		return time.Time{}, err
	}
	minute, err := field(5, 0, 0, 59, "minute")
	if err != nil {
		return time.Time{}, err
	}
	sec, err := field(6, 0, 0, 59, "second")
	if err != nil {
		return time.Time{}, err
	}

	loc := zone
	if m[7] != "" {
		off, err := parseOffset(m[7])
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid offset in %q: %w", s, err)
		}
		loc = off
	}

	// time.Date silently rolls 2026-02-30 into March; a probe catches it.
	probe := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if probe.Year() != year || probe.Month() != time.Month(month) || probe.Day() != day {
		return time.Time{}, fmt.Errorf("invalid date %q", s)
	}

	return time.Date(year, time.Month(month), day, hour, minute, sec, 0, loc), nil
}

func parseOffset(s string) (*time.Location, error) {
	if s == "Z" {
		return time.UTC, nil
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
	}
	body := s[1:]
	var hh, mm int
	var err error
	switch len(body) {
	case 2:
		hh, err = strconv.Atoi(body)
	case 4:
		hh, err = strconv.Atoi(body[:2])
		if err == nil {
			mm, err = strconv.Atoi(body[2:])
		}
	case 5:
		if body[2] != ':' {
			return nil, fmt.Errorf("malformed offset %q", s)
		}
		hh, err = strconv.Atoi(body[:2])
		if err == nil {
			mm, err = strconv.Atoi(body[3:])
		}
	default:
		return nil, fmt.Errorf("malformed offset %q", s)
	}
	if err != nil {
		return nil, err
	}
	if hh > 23 || mm > 59 {
		return nil, fmt.Errorf("offset out of range in %q", s)
	}
	secs := sign * (hh*3600 + mm*60)
	return time.FixedZone(s, secs), nil
}

// resolveZone returns the IANA location named by flag, or time.Local
// (which honours $TZ) when flag is empty. Unknown zone names are
// fatal — we never silently fall back to UTC.
func resolveZone(flag string) (*time.Location, error) {
	if flag == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(flag)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone %q: %w", flag, err)
	}
	return loc, nil
}
