#!/bin/bash
# benchmark-vs-git.sh — Compare dcfh performance against git
#
# Creates a single test directory and benchmarks both tools against the
# exact same data. The .git and .dcfh directories are removed between
# runs so neither tool's index files inflate the other's file count.
#
# Benchmarks:
#   - Initial index/add (full scan + hash of all files)
#   - Status check (no changes — clean working tree)
#   - Incremental update (10% modified files)
#
# Also captures pprof profiles and GC telemetry for dcfh.
#
# Usage:
#   ./scripts/benchmark-vs-git.sh [--profile] [--large]
#
# Options:
#   --profile   Capture CPU/memory profiles and GC trace
#   --large     Use larger dataset (~300MB instead of ~32MB)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DCFH="$REPO_ROOT/dcfh"

PROFILE=false
LARGE=false
for arg in "$@"; do
    case "$arg" in
        --profile) PROFILE=true ;;
        --large)   LARGE=true ;;
        *)         echo "Unknown option: $arg"; exit 1 ;;
    esac
done

# Ensure dcfh is built
if [ ! -x "$DCFH" ]; then
    echo "Building dcfh..."
    (cd "$REPO_ROOT" && make build >/dev/null 2>&1)
fi

# ─── Test data configuration ───────────────────────────────────────────────

if [ "$LARGE" = true ]; then
    TINY_COUNT=2000;  TINY_MIN=100;   TINY_MAX=1024
    SMALL_COUNT=3000; SMALL_MIN=1024; SMALL_MAX=51200
    MEDIUM_COUNT=400; MEDIUM_MIN=51200; MEDIUM_MAX=512000
    LARGE_COUNT=100;  LARGE_MIN=1048576; LARGE_MAX=5242880
    LABEL="large (~300MB)"
else
    TINY_COUNT=200;   TINY_MIN=100;   TINY_MAX=1024
    SMALL_COUNT=300;  SMALL_MIN=1024; SMALL_MAX=51200
    MEDIUM_COUNT=40;  MEDIUM_MIN=51200; MEDIUM_MAX=512000
    LARGE_COUNT=10;   LARGE_MIN=1048576; LARGE_MAX=5242880
    LABEL="standard (~32MB)"
fi

TOTAL_FILES=$((TINY_COUNT + SMALL_COUNT + MEDIUM_COUNT + LARGE_COUNT))
TESTDIR=$(mktemp -d "/tmp/dcfh-bench-XXXXXX")
REPODIR="$TESTDIR/repo"
BACKUPDIR="$TESTDIR/backup"
RESULTS_DIR="$REPO_ROOT/benchmark-results"
mkdir -p "$RESULTS_DIR" "$REPODIR"

trap 'rm -rf "$TESTDIR"' EXIT

echo "═══════════════════════════════════════════════════════════"
echo "  dcfh vs git benchmark ($LABEL)"
echo "  Test directory: $REPODIR"
echo "  Files: $TOTAL_FILES"
echo "═══════════════════════════════════════════════════════════"
echo ""

# ─── Generate test data ────────────────────────────────────────────────────

generate_files() {
    local dir="$1" prefix="$2" count="$3" min_size="$4" max_size="$5"
    mkdir -p "$dir"
    for i in $(seq 1 "$count"); do
        local size=$((min_size + RANDOM % (max_size - min_size + 1)))
        local subdir="$dir/d$((i % 20))"
        mkdir -p "$subdir"
        dd if=/dev/urandom of="$subdir/${prefix}_${i}.bin" bs=1 count="$size" 2>/dev/null
    done
}

echo -n "Generating test data..."
GENSTART=$(date +%s%N)

generate_files "$REPODIR" "tiny"   "$TINY_COUNT"   "$TINY_MIN"   "$TINY_MAX" &
generate_files "$REPODIR" "small"  "$SMALL_COUNT"  "$SMALL_MIN"  "$SMALL_MAX" &
generate_files "$REPODIR" "medium" "$MEDIUM_COUNT" "$MEDIUM_MIN" "$MEDIUM_MAX" &
generate_files "$REPODIR" "large"  "$LARGE_COUNT"  "$LARGE_MIN"  "$LARGE_MAX" &
wait

GENEND=$(date +%s%N)
GENMS=$(( (GENEND - GENSTART) / 1000000 ))
DATASIZE=$(du -sh "$REPODIR" | cut -f1)
FILECOUNT=$(find "$REPODIR" -type f | wc -l)
echo " done (${GENMS}ms, $DATASIZE, $FILECOUNT files)"

# Pre-select which files to modify for the incremental test (same for both tools)
MODIFY_COUNT=$((TOTAL_FILES / 10))
MODIFY_LIST="$TESTDIR/files-to-modify.txt"
find "$REPODIR" -type f | shuf -n "$MODIFY_COUNT" > "$MODIFY_LIST"

echo ""

# ─── Timing helper ─────────────────────────────────────────────────────────

# Stores result in global LAST_MS
LAST_MS=0
time_cmd() {
    local label="$1"
    shift
    local start end
    start=$(date +%s%N)
    "$@" >/dev/null 2>&1 || true
    end=$(date +%s%N)
    LAST_MS=$(( (end - start) / 1000000 ))
    printf "  %-35s %6d ms\n" "$label" "$LAST_MS"
}

# Modify the pre-selected files (overwrites first 100 bytes with random data)
modify_files() {
    while IFS= read -r f; do
        dd if=/dev/urandom of="$f" bs=100 count=1 conv=notrunc 2>/dev/null
    done < "$MODIFY_LIST"
}

# Back up the files that will be modified so we can restore them
backup_modified_files() {
    mkdir -p "$BACKUPDIR"
    while IFS= read -r f; do
        local relpath="${f#$REPODIR/}"
        local backdir="$BACKUPDIR/$(dirname "$relpath")"
        mkdir -p "$backdir"
        cp "$f" "$BACKUPDIR/$relpath"
    done < "$MODIFY_LIST"
}

# Restore modified files from backup
restore_modified_files() {
    while IFS= read -r f; do
        local relpath="${f#$REPODIR/}"
        cp "$BACKUPDIR/$relpath" "$f"
    done < "$MODIFY_LIST"
}

# Back up the files before any modifications
backup_modified_files

# ─── Benchmark git ─────────────────────────────────────────────────────────

echo "── git ──────────────────────────────────────────────────"

cd "$REPODIR"
git init -q .

time_cmd "git add . (initial)" git add .
GIT_INIT_MS=$LAST_MS

time_cmd "git status (clean)" git status
GIT_STATUS_CLEAN_MS=$LAST_MS

# Modify 10% of files
modify_files

time_cmd "git add . (incremental, ${MODIFY_COUNT} changed)" git add .
GIT_INCR_MS=$LAST_MS
echo ""

# Clean up git and restore files for dcfh run
rm -rf "$REPODIR/.git"
restore_modified_files

# ─── Benchmark dcfh ────────────────────────────────────────────────────────

echo "── dcfh ─────────────────────────────────────────────────"

cd "$REPODIR"
"$DCFH" init . >/dev/null 2>&1

time_cmd "dcfh update (initial)" "$DCFH" update
DCFH_INIT_MS=$LAST_MS

time_cmd "dcfh status (clean)" "$DCFH" status
DCFH_STATUS_CLEAN_MS=$LAST_MS

# Modify same 10% of files
modify_files

time_cmd "dcfh update (incremental, ${MODIFY_COUNT} changed)" "$DCFH" update
DCFH_INCR_MS=$LAST_MS

time_cmd "dcfh status (after update)" "$DCFH" status
DCFH_STATUS_AFTER_MS=$LAST_MS
echo ""

# Clean up dcfh
rm -rf "$REPODIR/.dcfh"
restore_modified_files

# ─── Comparison ────────────────────────────────────────────────────────────

echo "── Comparison ───────────────────────────────────────────"
printf "  %-25s %8s %8s %8s\n" "Operation" "git(ms)" "dcfh(ms)" "ratio"
printf "  %-25s %8s %8s %8s\n" "─────────────────────────" "────────" "────────" "────────"

ratio() {
    if [ "$1" -eq 0 ]; then echo "N/A"; else printf "%.2fx" "$(echo "$2 / $1" | bc -l)"; fi
}

printf "  %-25s %8d %8d %8s\n" "Initial add/update" "$GIT_INIT_MS" "$DCFH_INIT_MS" "$(ratio "$GIT_INIT_MS" "$DCFH_INIT_MS")"
printf "  %-25s %8d %8d %8s\n" "Status (clean)" "$GIT_STATUS_CLEAN_MS" "$DCFH_STATUS_CLEAN_MS" "$(ratio "$GIT_STATUS_CLEAN_MS" "$DCFH_STATUS_CLEAN_MS")"
printf "  %-25s %8d %8d %8s\n" "Incremental update" "$GIT_INCR_MS" "$DCFH_INCR_MS" "$(ratio "$GIT_INCR_MS" "$DCFH_INCR_MS")"
printf "  %-25s %8s %8s %8s\n" "" "────────" "────────" "────────"

# Aggregate: full workflow = init + status + incremental
GIT_TOTAL=$((GIT_INIT_MS + GIT_STATUS_CLEAN_MS + GIT_INCR_MS))
DCFH_TOTAL=$((DCFH_INIT_MS + DCFH_STATUS_CLEAN_MS + DCFH_INCR_MS))
printf "  %-25s %8d %8d %8s\n" "Aggregate (all 3)" "$GIT_TOTAL" "$DCFH_TOTAL" "$(ratio "$GIT_TOTAL" "$DCFH_TOTAL")"
echo ""

# ─── Profiling (optional) ─────────────────────────────────────────────────

if [ "$PROFILE" = true ]; then
    echo "── Profiling ────────────────────────────────────────────"

    # Fresh dcfh init for profiling
    cd "$REPODIR"
    "$DCFH" init . >/dev/null 2>&1

    # CPU + Memory profiles during update (initial scan — most allocation-heavy)
    echo "  Capturing CPU + memory profiles during update..."
    DCFH_CPUPROFILE="$RESULTS_DIR/cpu.prof" DCFH_MEMPROFILE="$RESULTS_DIR/mem.prof" "$DCFH" update >/dev/null 2>&1 || true
    CPU_SIZE=$(stat -c%s "$RESULTS_DIR/cpu.prof" 2>/dev/null || echo 0)
    MEM_SIZE=$(stat -c%s "$RESULTS_DIR/mem.prof" 2>/dev/null || echo 0)
    echo "  → $RESULTS_DIR/cpu.prof (${CPU_SIZE} bytes)"
    echo "  → $RESULTS_DIR/mem.prof (${MEM_SIZE} bytes)"

    # Clean up for GC trace run
    rm -rf "$REPODIR/.dcfh"

    # GC trace
    echo "  Capturing GC trace..."
    "$DCFH" init . >/dev/null 2>&1
    GODEBUG=gctrace=1 "$DCFH" update >/dev/null 2>"$RESULTS_DIR/gc-trace.log"
    echo "  → $RESULTS_DIR/gc-trace.log"

    # Clean up
    rm -rf "$REPODIR/.dcfh"

    # Parse GC trace
    GC_CYCLES=$(grep -c "^gc " "$RESULTS_DIR/gc-trace.log" 2>/dev/null || echo 0)
    if [ "$GC_CYCLES" -gt 0 ]; then
        # Extract pause times (field 5 is the pause in ms)
        TOTAL_PAUSE=$(grep "^gc " "$RESULTS_DIR/gc-trace.log" | awk -F'[+ ]' '{sum += $5} END {printf "%.1f", sum}' 2>/dev/null || echo "N/A")
        HEAP_MAX=$(grep "^gc " "$RESULTS_DIR/gc-trace.log" | awk -F'[->]' '{print $2}' | awk -F'[( ]' '{print $1}' | sort -n | tail -1 2>/dev/null || echo "N/A")
        echo ""
        echo "  GC Summary:"
        printf "    %-25s %s\n" "GC cycles:" "$GC_CYCLES"
        printf "    %-25s %s ms\n" "Total GC pause:" "$TOTAL_PAUSE"
        printf "    %-25s %s\n" "Peak heap:" "$HEAP_MAX"
        if [ "$GC_CYCLES" -gt 50 ]; then
            echo "    ⚠  WARNING: >50 GC cycles suggests GC thrashing"
        else
            echo "    ✓  GC pressure looks reasonable"
        fi
    else
        echo "  No GC cycles recorded"
    fi

    echo ""
    echo "  To analyse profiles:"
    echo "    go tool pprof -http=:8080 $RESULTS_DIR/cpu.prof"
    echo "    go tool pprof -http=:8081 $RESULTS_DIR/mem.prof"
    echo ""
fi

echo "═══════════════════════════════════════════════════════════"
echo "  Benchmark complete"
echo "═══════════════════════════════════════════════════════════"
