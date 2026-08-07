# Test Optimization Plan

**Goal:** Reduce the test suite wall-clock time from ~30s to ~10-12s for local development.

**Priorities:**
1. **Parallelize tests** — add `t.Parallel()` where safe (tests that don't mutate global state).
2. **Support `-short`** — skip heavy integration tests (Lua, 7z, external processes) during local runs.
3. **Refactor global singletons** — gradually replace globals (`AppConfig`, `GlobalHotkeysMgr`, `MacroMgr`) with injected dependencies to simplify parallelization.

**Current step (patch #1):** Add the plan document. No test code is changed yet.

**Criteria for adding `t.Parallel()`:**
- The test does not modify global variables without restoring them.
- The test uses `t.TempDir()` or isolated temporary resources.
- The test creates its own `vtui.FrameManager` and closes it.
- The test does **not** use `t.Setenv` or `t.Chdir`.
- The test does not rely on global clipboard or PTY state.

**Exclusions (not parallelized yet):**
- Tests that modify `AppConfig` without full restore.
- Tests using global managers (`GlobalHotkeysMgr`, `MacroMgr`) without save/restore.
- `dialog_layouts_test.go` (complex multilingual loop).
- Any test that uses `t.Setenv` or `t.Chdir`.

**Expected effect:** 40-50% reduction in `main` package test time, once parallelization is fully applied.

**Next steps:**
- Add `-short` flag to skip integration tests.
- Refactor global singletons in tests (use `TestMain` or per‑test init with restore).
- Profile to identify the slowest tests.
- Add `t.Parallel()` to individual tests incrementally, verifying each one.