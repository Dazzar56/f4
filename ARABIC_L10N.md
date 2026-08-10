# Arabic Localization Progress Tracking

This document tracks the progressive implementation of the Arabic translation (`ar`) for f4.

## Current Progress
* **Step 1**: (Completed)
  * Registered the Arabic language (`ar` with `whatlanggo.Ara`) in the test linter `lang_consistency_test.go`.
  * Translated first half of the interface strings (up to `Action.Editor.Redo`) in `lang/ar.lng`.

## Next Steps
* **Step 2**: Translate the remaining interface strings in `lang/ar.lng` (starting from `Action.Editor.Redo.Desc` to the end of the file).
* **Step 3**: Create the help file `help/ar.hlf` and translate global help topics (`@Contents`, `@Panels`, etc.).
* **Step 4**: Translate VisRen help sections Part 1.
* **Step 5**: Translate VisRen help sections Part 2.
* **Step 6**: Translate VisRen help sections Part 3 and update test baselines.