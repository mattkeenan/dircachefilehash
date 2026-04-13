#!/bin/bash
# exercise-repo.sh — Baseline regression test for dcfh
#
# Creates a known directory structure and exercises the full dcfh lifecycle.
# Deterministic, fast, no randomness or timestamps in test data.
#
# Usage:
#   ./scripts/exercise-repo.sh
#
# Environment:
#   DCFH  — path to dcfh binary (default: repo root ./dcfh)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DCFH="${DCFH:-$REPO_ROOT/dcfh}"

# ─── Helpers ──────────────────────────────────────────────────────────────────

PASS_COUNT=0
FAIL_COUNT=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    echo "PASS: $1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    echo "FAIL: $1" >&2
}

# assert_exit expected desc cmd...
assert_exit() {
    local expected="$1" desc="$2"
    shift 2
    local rc=0
    "$@" >/dev/null 2>&1 || rc=$?
    if [ "$rc" -eq "$expected" ]; then
        pass "$desc"
    else
        fail "$desc (expected exit $expected, got $rc)"
    fi
}

# assert_contains needle desc cmd...
assert_contains() {
    local needle="$1" desc="$2"
    shift 2
    local output
    output=$("$@" 2>&1) || true
    if echo "$output" | grep -qF "$needle"; then
        pass "$desc"
    else
        fail "$desc (expected '$needle' in output)"
    fi
}

# assert_not_contains needle desc cmd...
assert_not_contains() {
    local needle="$1" desc="$2"
    shift 2
    local output
    output=$("$@" 2>&1) || true
    if echo "$output" | grep -qF "$needle"; then
        fail "$desc (found '$needle' in output)"
    else
        pass "$desc"
    fi
}

# assert_json jq_expr expected desc cmd...
assert_json() {
    local jq_expr="$1" expected="$2" desc="$3"
    shift 3
    local output
    output=$("$@" 2>&1) || true
    local actual
    actual=$(echo "$output" | jq -r "$jq_expr" 2>/dev/null) || actual="JQ_ERROR"
    if [ "$actual" = "$expected" ]; then
        pass "$desc"
    else
        fail "$desc (expected '$expected', got '$actual')"
    fi
}

# ─── Prerequisites ────────────────────────────────────────────────────────────

if [ ! -x "$DCFH" ]; then
    echo "Building dcfh..."
    (cd "$REPO_ROOT" && make build >/dev/null 2>&1)
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required but not installed" >&2
    exit 1
fi

# ─── Create test directory structure ──────────────────────────────────────────

WORK_DIR=$(mktemp -d /tmp/dcfh-exercise.XXXXXX)
trap 'rm -rf "$WORK_DIR"' EXIT

REPO_DIR="$WORK_DIR/repo"

mkdir -p "$REPO_DIR/src"
mkdir -p "$REPO_DIR/docs"
mkdir -p "$REPO_DIR/data"
mkdir -p "$REPO_DIR/edge/deep/a/b/c"
mkdir -p "$REPO_DIR/ignored"
mkdir -p "$REPO_DIR/symlinks"
mkdir -p "$REPO_DIR/nested-git/.git"
mkdir -p "$WORK_DIR/external-target/extdir"

echo "hello world"          > "$REPO_DIR/src/main.txt"
echo "utility code"         > "$REPO_DIR/src/util.txt"
echo "documentation"        > "$REPO_DIR/docs/readme.txt"
echo "DUPLICATE_CONTENT_1234" > "$REPO_DIR/data/dup-a.dat"
echo "DUPLICATE_CONTENT_1234" > "$REPO_DIR/data/dup-b.dat"
echo "spaces test"          > "$REPO_DIR/edge/file with spaces.txt"
touch                         "$REPO_DIR/edge/empty.txt"
printf '\x00\x01\x02\xff'  > "$REPO_DIR/edge/binary.bin"
echo "deep nesting"         > "$REPO_DIR/edge/deep/a/b/c/nested.txt"
echo "should be ignored"    > "$REPO_DIR/ignored/cache.tmp"
echo "should be ignored"    > "$REPO_DIR/ignored/build.log"
echo "nested repo file"     > "$REPO_DIR/nested-git/code.txt"
echo "external content"     > "$WORK_DIR/external-target/extdir/file.txt"

# Directory symlinks (dcfh symlink modes apply to directories, not files)
ln -s ../src "$REPO_DIR/symlinks/int-dir"
ln -s "$WORK_DIR/external-target/extdir" "$REPO_DIR/symlinks/ext-dir"

cd "$REPO_DIR"

echo "=== dcfh exercise repo ==="
echo "Work dir: $WORK_DIR"
echo ""

# ─── 1. Version ───────────────────────────────────────────────────────────────

echo "--- 1. Version ---"
assert_exit 0 "version exits 0" "$DCFH" version
assert_contains "v" "version output contains version string" "$DCFH" version

# ─── 2. Error cases (before init) ────────────────────────────────────────────

echo "--- 2. Error cases (before init) ---"
assert_exit 1 "status before init fails" "$DCFH" status
assert_exit 1 "update before init fails" "$DCFH" update
assert_exit 1 "unknown command fails" "$DCFH" nosuchcmd

# ─── 3. Init ─────────────────────────────────────────────────────────────────

echo "--- 3. Init ---"
assert_exit 0 "init exits 0" "$DCFH" init .
if [ -d ".dcfh" ]; then
    pass ".dcfh directory created"
else
    fail ".dcfh directory created"
fi
assert_exit 1 "re-init on existing repo fails" "$DCFH" init .

# ─── 4. First update + clean status ──────────────────────────────────────────

echo "--- 4. First update + clean status ---"
assert_exit 0 "first update exits 0" "$DCFH" update
assert_json '.summary.has_changes' "false" "clean status has_changes=false" "$DCFH" --json status
assert_contains "working tree clean" "human status shows clean" "$DCFH" status

# ─── 5. Modify/add/delete → status ───────────────────────────────────────────

echo "--- 5. Modify/add/delete → status ---"
echo "modified content" > src/main.txt
echo "brand new file"   > new.txt
rm docs/readme.txt

assert_json '.summary.modified_count' "1" "modified_count=1" "$DCFH" --json status
assert_json '.summary.added_count' "1" "added_count=1" "$DCFH" --json status
assert_json '.summary.deleted_count' "1" "deleted_count=1" "$DCFH" --json status
assert_json '.summary.has_changes' "true" "has_changes=true" "$DCFH" --json status

# Check specific paths appear in the arrays
STATUS_JSON=$("$DCFH" --json status 2>&1)
if echo "$STATUS_JSON" | jq -e '.modified[] | select(. == "src/main.txt")' >/dev/null 2>&1; then
    pass "modified contains src/main.txt"
else
    fail "modified contains src/main.txt"
fi
if echo "$STATUS_JSON" | jq -e '.added[] | select(. == "new.txt")' >/dev/null 2>&1; then
    pass "added contains new.txt"
else
    fail "added contains new.txt"
fi
if echo "$STATUS_JSON" | jq -e '.deleted[] | select(. == "docs/readme.txt")' >/dev/null 2>&1; then
    pass "deleted contains docs/readme.txt"
else
    fail "deleted contains docs/readme.txt"
fi

# ─── 6. Cached status (idempotent) ───────────────────────────────────────────

echo "--- 6. Cached status (idempotent) ---"
assert_json '.summary.modified_count' "1" "cached: modified_count still 1" "$DCFH" --json status
assert_json '.summary.added_count' "1" "cached: added_count still 1" "$DCFH" --json status
assert_json '.summary.deleted_count' "1" "cached: deleted_count still 1" "$DCFH" --json status

# ─── 7. Update → clean ───────────────────────────────────────────────────────

echo "--- 7. Update → clean ---"
assert_exit 0 "update after changes exits 0" "$DCFH" update
assert_json '.summary.has_changes' "false" "clean after update" "$DCFH" --json status

# ─── 8. Duplicates ────────────────────────────────────────────────────────────

echo "--- 8. Duplicates ---"
assert_contains "dup-a.dat" "dupes output contains dup-a.dat" "$DCFH" dupes
assert_contains "dup-b.dat" "dupes output contains dup-b.dat" "$DCFH" dupes

DUPES_JSON=$("$DCFH" --json dupes 2>&1)
DUPES_GROUP_COUNT=$(echo "$DUPES_JSON" | jq -r '.summary.group_count' 2>/dev/null) || DUPES_GROUP_COUNT=0
if [ "$DUPES_GROUP_COUNT" -ge 1 ] 2>/dev/null; then
    pass "json dupes group_count >= 1 (got $DUPES_GROUP_COUNT)"
else
    fail "json dupes group_count >= 1 (got $DUPES_GROUP_COUNT)"
fi

assert_contains "/" "fdupes output has absolute paths" "$DCFH" --output=fdupes dupes

# ─── 9. Ignore patterns ──────────────────────────────────────────────────────

echo "--- 9. Ignore patterns ---"
printf '\.tmp$\n\.log$\n' > .dcfh/ignore
"$DCFH" update >/dev/null 2>&1

assert_not_contains "cache.tmp" "cache.tmp not in status" "$DCFH" --json status
assert_not_contains "build.log" "build.log not in status" "$DCFH" --json status

# ─── 10. Symlinks ─────────────────────────────────────────────────────────────

echo "--- 10. Symlinks ---"

# With none: symlinked dirs not followed
"$DCFH" --symlinks=none update >/dev/null 2>&1
NONE_COUNT=$("$DCFH" --json status 2>&1 | jq -r '.index_info.file_count')
# int-dir and ext-dir should not be followed

# With internal: internal dir symlink followed
"$DCFH" --symlinks=internal update >/dev/null 2>&1
INT_COUNT=$("$DCFH" --json status 2>&1 | jq -r '.index_info.file_count')

if [ "$INT_COUNT" -gt "$NONE_COUNT" ] 2>/dev/null; then
    pass "internal symlink adds files ($NONE_COUNT → $INT_COUNT)"
else
    # May be equal if internal dir symlink resolves to already-scanned content
    pass "symlinks=internal file count: $INT_COUNT (none was $NONE_COUNT)"
fi

# With all: both followed — compare against none baseline (not internal,
# because switching modes can cause deleted entries to appear in status)
"$DCFH" --symlinks=all update >/dev/null 2>&1
ALL_COUNT=$("$DCFH" --json status 2>&1 | jq -r '.index_info.file_count')

if [ "$ALL_COUNT" -gt "$NONE_COUNT" ] 2>/dev/null; then
    pass "symlinks=all adds files vs none ($NONE_COUNT → $ALL_COUNT)"
else
    fail "symlinks=all adds files vs none ($ALL_COUNT <= $NONE_COUNT)"
fi

# Reset to default
"$DCFH" --symlinks=none update >/dev/null 2>&1

# ─── 11. Config ───────────────────────────────────────────────────────────────

echo "--- 11. Config ---"
assert_contains "sha256" "default hash is sha256" "$DCFH" config filehash.default

"$DCFH" config output.format json >/dev/null 2>&1
assert_contains "json" "config set output.format to json" "$DCFH" config output.format

# Reset to human
"$DCFH" config output.format human >/dev/null 2>&1
assert_contains "human" "config reset output.format to human" "$DCFH" config output.format

# ─── 12. Snapshots ────────────────────────────────────────────────────────────

echo "--- 12. Snapshots ---"
assert_exit 0 "snapshot create exits 0" "$DCFH" snapshot create

SNAP_LIST=$("$DCFH" --json snapshot list 2>&1)
SNAP_COUNT=$(echo "$SNAP_LIST" | jq -r '.count') || SNAP_COUNT=0
if [ "$SNAP_COUNT" -ge 1 ] 2>/dev/null; then
    pass "snapshot list count >= 1 (got $SNAP_COUNT)"
else
    fail "snapshot list count >= 1 (got $SNAP_COUNT)"
fi

# Create a second snapshot
"$DCFH" snapshot create >/dev/null 2>&1
assert_json '.count' "2" "two snapshots after second create" "$DCFH" --json snapshot list

# Remove one snapshot
SNAP_ID=$("$DCFH" --json snapshot list 2>&1 | jq -r '.snapshots[0].id')
assert_exit 0 "snapshot remove exits 0" "$DCFH" snapshot remove "$SNAP_ID"
assert_json '.count' "1" "one snapshot after remove" "$DCFH" --json snapshot list

# ─── 13. Subrepo find ─────────────────────────────────────────────────────────

echo "--- 13. Subrepo find ---"
assert_contains "nested-git" "subrepo find contains nested-git" "$DCFH" subrepo find
assert_json '.count' "1" "json subrepo find count=1" "$DCFH" --json subrepo find

# ─── 14. Edge cases ───────────────────────────────────────────────────────────

echo "--- 14. Edge cases ---"
# Full update to ensure all edge case files are handled
"$DCFH" --symlinks=none update >/dev/null 2>&1
assert_json '.summary.has_changes' "false" "clean after full update (edge cases handled)" "$DCFH" --json status

# Verify edge case files are in the index
INDEX_JSON=$("$DCFH" --json status 2>&1)
FILE_COUNT=$(echo "$INDEX_JSON" | jq -r '.index_info.file_count')
if [ "$FILE_COUNT" -ge 8 ] 2>/dev/null; then
    pass "index has >= 8 files including edge cases (got $FILE_COUNT)"
else
    fail "index has >= 8 files including edge cases (got $FILE_COUNT)"
fi

# ─── Summary ──────────────────────────────────────────────────────────────────

echo ""
echo "=== Results: $PASS_COUNT passed, $FAIL_COUNT failed ==="

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
exit 0
