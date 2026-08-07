# Test Optimization Plan

**Goal:** Reduce the test suite wall-clock time from ~30s to ~10s for local development.

**Current baseline (from `run_all_tests.sh`):** ~26.8s for ~500 tests.

**Top offenders (from the analysis log):**

| Test | Time | Issue |
|------|------|-------|
| `TestAllDialogs_LayoutValidation/Settings.Colorer` | 2.09s | Colorer schema generation |
| `TestAllDialogs_LayoutValidation/Settings.Editor` | 1.40s | Editor settings dialog |
| `TestPanelsFrame_DriveMenuBookmarkKeys` | 1.42s | Uses `t.Setenv` + `t.Parallel` (panic) |
| `TestLayout_F4ActionDialogs_Validity/EditorSettingsDialog` | 0.63s | Dialog setup |
| `TestTerminalView_ProcessFar2lInteract_ConcurrentRace` | 1.51s | Artificial delays for race detection |
| `TestIssue117_OSC52_Read_SecurityDenial` | 1.53s | Security timeouts |
| `TestAsyncBuffer_ContextRace` | 1.05s | Race condition test |

**Priorities:**
1. **Cache Colorer schemas** — reuse generated schemas across dialog tests (biggest win, ~2s).
2. **Parallelize safe tests** — add `t.Parallel()` only where there is no global state.
3. **Replace `time.Sleep` with `assert.Eventually`** in async tests.
4. **Remove `t.Parallel()` from tests that use `t.Setenv` / `t.Chdir`** (or restructure them).

**Current step (patch #3):** Update the plan with the detailed analysis.

**Next step (patch #4):** Implement Colorer schema caching in `colorer_plugin_test.go` and `dialog_layouts_test.go`.

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

**Next steps (detailed):**
- Add `-short` flag to skip integration tests (Lua, 7z, external processes).
- Refactor global singletons in tests (use `TestMain` or per‑test init with restore).
- Profile to identify the slowest tests.
- Add `t.Parallel()` to individual tests incrementally, verifying each one.