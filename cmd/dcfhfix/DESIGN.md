# dcfhfix Design Document

## Overview

`dcfhfix` is a targeted repair and editing tool for dcfh index files. It provides both standalone functionality and powerful integration with `dcfhfind` for bulk operations.

## Core Design Principle

**dcfhfix is designed to be usable standalone, but its true power comes from combination with dcfhfind.**

The combination allows for powerful workflows:
1. **dcfhfind**: Find and select entries using complex criteria
2. **dcfhfix**: Bulk edit the selected entries

## Integration Patterns

### Basic Workflow
```bash
# Find entries, then edit them
dcfhfind main --name "*.tmp" --print0 | xargs -0 dcfhfix main.idx entry remove
dcfhfind cache --size +100M --print | xargs -I {} dcfhfix cache.idx entry edit mode 0644 {}
```

### Advanced Workflows
```bash
# Find corrupted entries and fix permissions
dcfhfind all --corrupt --print | while read path; do
    dcfhfix main.idx entry edit mode 0644 "$path"
done

# Bulk update ownership for specific file patterns
dcfhfind main --name "*.log" --uid 0 --print | \
    xargs -I {} dcfhfix main.idx entry edit uid 1000 gid 1000 {}

# Remove all deleted entries from cache
dcfhfind cache --deleted --print | \
    xargs dcfhfix cache.idx entry remove
```

## Command Categories

### Standalone Operations
- **Inspection**: `dcfhfix <index> header show`, `dcfhfix <index> entry show <paths>`
- **Direct editing**: `dcfhfix <index> header edit <field> <value>`
- **Backup management**: `dcfhfix <index> fixes list/pop/discard/clear`

### Bulk Operations (via dcfhfind integration)
- **Bulk field updates**: Edit fields across multiple entries found by dcfhfind
- **Bulk removal**: Remove entries matching dcfhfind criteria
- **Batch corrections**: Fix common issues across selected entries

## Design Considerations

### Path Handling
- **Entry identification**: Entries are identified by their relative path within the repository
- **Multiple paths**: All entry commands accept multiple paths for bulk operations
- **Path validation**: Paths are validated against index contents before editing

### Safety Features
- **FIFO backup stack**: Every modification creates a backup for easy rollback
- **Dry-run support**: Preview changes before applying them
- **Atomic operations**: All modifications use temp files with atomic rename
- **Validation**: Field values are validated before writing

### Performance
- **Skiplist integration**: Efficient entry lookup for bulk operations
- **Single index load**: Index is loaded once and reused for multiple path operations
- **Minimal memory**: Zero-copy operations where possible

### Output Formats
- **Human-readable**: Default format for interactive use
- **JSON**: Machine-readable format for scripting and integration
- **Consistent**: Same format options across show commands

## Command Structure

### Header Commands
```bash
dcfhfix <index> header show [--format=human|json]
dcfhfix <index> header edit <field> <value>
dcfhfix <index> header edit json <json-data>
```

### Entry Commands
```bash
dcfhfix <index> entry show <path>... [--format=human|json]
dcfhfix <index> entry edit <field> <value> <path>...
dcfhfix <index> entry edit json <json-data> <path>...
dcfhfix <index> entry append <json-data>
dcfhfix <index> entry remove <path>...
dcfhfix <index> entry resort
```

### Backup Management
```bash
dcfhfix <index> fixes list [--format=human|json]
dcfhfix <index> fixes pop [--dry-run]
dcfhfix <index> fixes discard [--dry-run]
dcfhfix <index> fixes clear [--dry-run]
```

## Integration Examples

### Common Patterns

**Fix ownership issues**:
```bash
# Find files owned by wrong user and fix them
dcfhfind main --uid 0 --print | \
    xargs -I {} dcfhfix main.idx entry edit uid 1000 {}
```

**Clean up temporary files**:
```bash
# Remove all .tmp files from index
dcfhfind main --name "*.tmp" --print | \
    xargs dcfhfix main.idx entry remove
```

**Bulk permission fixes**:
```bash
# Set all .sh files to executable
dcfhfind main --name "*.sh" --print | \
    xargs -I {} dcfhfix main.idx entry edit mode 0755 {}
```

**Metadata corrections**:
```bash
# Fix timestamp issues for specific directory
dcfhfind main --path "build/*" --print | \
    xargs -I {} dcfhfix main.idx entry edit mtime $(date +%s) {}
```

### JSON-based bulk operations
```bash
# Complex multi-field updates
dcfhfind main --name "*.log" --print | \
    xargs -I {} dcfhfix main.idx entry edit json '{"uid":1000,"gid":1000,"mode":420}' {}
```

## Error Handling

### Validation
- **Field validation**: Type checking and range validation for all fields
- **Path validation**: Ensure paths exist in index before editing
- **JSON validation**: Validate JSON syntax and field types

### Recovery
- **Backup restoration**: Easy rollback with `fixes pop`
- **Partial failure handling**: Continue processing remaining paths on individual failures
- **Clear error messages**: Specific error reporting for debugging

## Future Enhancements

### Potential Integration Improvements
- **Pipeline mode**: Accept paths from stdin for seamless dcfhfind integration
- **Batch JSON mode**: Accept JSON arrays for bulk operations
- **Transaction support**: Group related changes into atomic transactions

### Advanced Features
- **Field templates**: Predefined field update templates
- **Conditional edits**: Apply changes only if conditions are met
- **Audit trail**: Enhanced logging of all modifications