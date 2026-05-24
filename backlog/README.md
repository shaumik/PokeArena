# Backlog — diary, not a tracker

**Action items live in [GitHub Issues](https://github.com/shaumik/PokeArena/issues).**
This folder is for chronological thoughts and progress notes — what was
considered, why a path was taken, what surprised us in a session. It is
read top-to-bottom (or rather: oldest-to-newest) by future humans and
agents trying to understand how the codebase got here.

## Convention

One file per entry. Filename **is** the timestamp:

```
YYYY-MM-DDTHH-MM-<short-slug>.md
```

- ISO-8601 date, dashes in the time half so the filename is shell-safe.
- Slug is 2–5 words, kebab-case, describing the entry topic.
- Lexicographic sort = chronological order. `ls backlog/` is the timeline.

Inside each file: free-form. A heading, then prose. Link out to issues
(`#42`), commits (`abc123`), or docs (`docs/mcp-protocol.md`) wherever
relevant. **Do not** edit old entries to "fix" them — write a new entry
that supersedes the old thinking. The diary is append-only on purpose;
the value is seeing the change of mind, not the final position.

## What goes where

| Thing                                   | Where               |
|-----------------------------------------|---------------------|
| "We should build X"                     | GitHub Issue        |
| Stable design contract                  | `docs/`             |
| "Here's what I tried and what happened" | Diary entry here    |
| "Today I learned the engine actually …" | Diary entry here    |
| Bug we hit and how we fixed it          | Commit message + PR |
| Roadmap / priorities                    | Issue labels + milestones |

If an entry is generating real action items, file an issue and reference
it from the entry. The entry records the thinking; the issue records the
work.
