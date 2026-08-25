# Project instructions

## Go build cache

- Use the system Go build cache reported by `go env GOCACHE` for all Go builds and tests.
- Do not redirect `GOCACHE` to `/tmp`, the repository, or another task-local directory unless the user explicitly asks for it.
- If the system cache is unavailable or not writable, report that constraint instead of silently creating a substitute cache.

