Start a new development session by creating a session file in `.claude/sessions/` with the format `YYYY-MM-DDTHHMM-$ARGUMENTS.md` (or just `YYYY-MM-DDTHHMM.md` if no name provided).

The session file should begin with:
1. Session name and ISO 8601 UTC timestamp as the title
2. Session overview section with start time in ISO 8601 UTC format
3. Goals section (ask user for goals if not clear)
4. Empty progress section ready for updates

Use `date -u '+%Y-%m-%dT%H:%M:%SZ'` for ISO 8601 UTC timestamps throughout.

After creating the file, create or update `.claude/sessions/.current-session` to track the active session filename.

Confirm the session has started and remind the user they can:
- Update it with `/project:session-update`
- End it with `/project:session-end`