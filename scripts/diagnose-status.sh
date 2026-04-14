#!/bin/bash
# diagnose-status.sh — Profile dcfh status to find performance bottlenecks
#
# Runs dcfh status with both Go pprof and strace, then summarises findings.
# Designed to answer: where is the per-entry latency coming from?
#
# Usage:
#   ./scripts/diagnose-status.sh [target-dir]
#
# If target-dir is omitted, uses current directory.
# Requires: strace, go tool pprof

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DCFH="${DCFH:-$REPO_ROOT/dcfh}"
TARGET="${1:-.}"
DIAG_DIR=$(mktemp -d /tmp/dcfh-diag.XXXXXX)

if [ ! -x "$DCFH" ]; then
    echo "Error: dcfh binary not found at $DCFH" >&2
    echo "Run 'make build' first" >&2
    exit 1
fi

if [ ! -d "$TARGET/.dcfh" ]; then
    echo "Error: $TARGET is not a dcfh repository (no .dcfh directory)" >&2
    exit 1
fi

if ! command -v strace >/dev/null 2>&1; then
    echo "Error: strace is required but not installed" >&2
    exit 1
fi

echo "=== dcfh status diagnostics ==="
echo "Target: $(cd "$TARGET" && pwd)"
echo "Output: $DIAG_DIR"
echo ""

# Phase 1: CPU profile
echo "--- Phase 1: CPU profile ---"
cd "$TARGET"
DCFH_CPUPROFILE="$DIAG_DIR/cpu.prof" "$DCFH" status > /dev/null 2>&1 || true
if [ -f "$DIAG_DIR/cpu.prof" ]; then
    echo "CPU profile written to $DIAG_DIR/cpu.prof"
else
    echo "Warning: CPU profile not generated"
fi
echo ""

# Phase 2: strace with relative timestamps
echo "--- Phase 2: strace (this may take a while on large repos) ---"
cd "$TARGET"
strace -r -f -o "$DIAG_DIR/strace.log" "$DCFH" status > /dev/null 2>&1 || true
STRACE_LINES=$(wc -l < "$DIAG_DIR/strace.log")
echo "strace captured $STRACE_LINES lines"
echo ""

# Phase 3: Analyse
echo "=== CPU Profile Top 20 (cumulative) ==="
if [ -f "$DIAG_DIR/cpu.prof" ]; then
    go tool pprof -top -cum "$DCFH" "$DIAG_DIR/cpu.prof" 2>/dev/null | head -30 || echo "(pprof analysis failed)"
else
    echo "(no CPU profile available)"
fi

echo ""
echo "=== Strace: Slowest individual syscalls (>1ms) ==="
awk '{
    # Match lines like: [pid 12345]      0.001234 syscall(
    # or standalone:          0.001234 syscall(
    if (match($0, /([0-9]+\.[0-9]+) +([a-z_]+)\(/, m)) {
        t = m[1]+0
        if (t > 0.001) printf "%10.6f  %s\n", t, m[2]
    }
}' "$DIAG_DIR/strace.log" | sort -rn | head -20

echo ""
echo "=== Strace: Time between syscalls >1ms (userspace time) ==="
awk '{
    if (match($0, /\[pid *([0-9]+)\] *([0-9]+\.[0-9]+)/, m)) {
        pid = m[1]; t = m[2]+0
        if (t > 0.001) {
            # Extract the syscall name
            rest = substr($0, RSTART+RLENGTH)
            if (match(rest, /([a-z_]+)\(/, sc)) {
                printf "%10.6f  pid=%-8s %s\n", t, pid, sc[1]
            } else {
                printf "%10.6f  pid=%-8s (resumed)\n", t, pid
            }
        }
    }
}' "$DIAG_DIR/strace.log" | sort -rn | head -20

echo ""
echo "=== Strace: Syscalls per thread (top 10) ==="
grep -oP '\[pid \K[0-9]+' "$DIAG_DIR/strace.log" 2>/dev/null | sort | uniq -c | sort -rn | head -10 || \
    echo "  (single-threaded trace, no pid tags)"

echo ""
echo "=== Strace: Total syscall counts by type (top 15) ==="
grep -oP '[0-9]\.[0-9]+ +\K[a-z_]+(?=\()' "$DIAG_DIR/strace.log" 2>/dev/null | sort | uniq -c | sort -rn | head -15 || \
    grep -oP '^\s*[0-9.]+ \K[a-z_]+(?=\()' "$DIAG_DIR/strace.log" | sort | uniq -c | sort -rn | head -15

echo ""
echo "=== Strace: futex wait time distribution ==="
awk '{
    if (match($0, /([0-9]+\.[0-9]+) +<\.\.\. futex resumed/, m)) {
        t = m[1]+0
        if (t < 0.0001) bucket["<0.1ms"]++
        else if (t < 0.001) bucket["0.1-1ms"]++
        else if (t < 0.01) bucket["1-10ms"]++
        else if (t < 0.1) bucket["10-100ms"]++
        else bucket[">100ms"]++
        total++
    }
}
END {
    if (total > 0) {
        printf "  %-12s %8d (%5.1f%%)\n", "<0.1ms", bucket["<0.1ms"]+0, (bucket["<0.1ms"]+0)/total*100
        printf "  %-12s %8d (%5.1f%%)\n", "0.1-1ms", bucket["0.1-1ms"]+0, (bucket["0.1-1ms"]+0)/total*100
        printf "  %-12s %8d (%5.1f%%)\n", "1-10ms", bucket["1-10ms"]+0, (bucket["1-10ms"]+0)/total*100
        printf "  %-12s %8d (%5.1f%%)\n", "10-100ms", bucket["10-100ms"]+0, (bucket["10-100ms"]+0)/total*100
        printf "  %-12s %8d (%5.1f%%)\n", ">100ms", bucket[">100ms"]+0, (bucket[">100ms"]+0)/total*100
        printf "  Total: %d futex waits\n", total
    } else {
        print "  (no futex resume data found)"
    }
}' "$DIAG_DIR/strace.log"

echo ""
echo "=== Summary ==="
echo "Diagnostic files:"
echo "  $DIAG_DIR/cpu.prof   — go tool pprof $DCFH $DIAG_DIR/cpu.prof"
echo "  $DIAG_DIR/strace.log — raw strace output ($STRACE_LINES lines)"
echo ""
echo "Interactive analysis:"
echo "  go tool pprof -http=:8080 $DCFH $DIAG_DIR/cpu.prof"
echo "  go tool pprof $DCFH $DIAG_DIR/cpu.prof"
