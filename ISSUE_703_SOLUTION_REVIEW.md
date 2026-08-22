# Issue #703 solution review

## Problem model

`App.CopyWindowTitle` was wired to `currentWindowTitle()`. That function
deliberately reports the host terminal/GUI title generated from
`ConsoleTitleTemplate` and uses the active workspace title, ignoring modal
frames. As a result, invoking the action from a dialog copied the underlying
workspace state (for example, `Panels`) instead of the dialog identity (for
example, `User Menu`).

The fix must distinguish the host title used by `UpdateWindowTitle` and
`Far.Title` from the active UI frame title used by the debugging clipboard
action.

## Candidate solutions

1. Change `currentWindowTitle()` to return the top frame title. Rejected: this
   would change the visible terminal/GUI title and the documented `Far.Title`
   API, and would make transient menus/dialogs leak into host window titles.
2. Add a separate `currentFrameTitle()` helper using
   `FrameManager.GetTopFrame().GetTitle()`, trim decorative spaces, and use it
   only in `App.CopyWindowTitle`, with the existing host-title function as a
   startup/shutdown fallback. Selected: it follows the requested identity and
   keeps the existing host-title contract unchanged.
3. Add a new vtui API that separately exposes the active frame identity.
   Rejected for this bug: `GetTopFrame().GetTitle()` already supplies the
   needed stable API, so a cross-repository dependency change would add scope
   without improving correctness.

## Three-pass review

### Pass 1: correctness

The action now reads the exact frame that receives input. A normal workspace
returns `Desktop`/`Panels`; a modal dialog returns its own title, such as
`User Menu`. `TrimSpace` removes only border-padding used by vtui titles.

### Pass 2: lifecycle and edge cases

The helper tolerates an uninitialized frame manager and an empty frame stack.
The action falls back to the host title during startup/shutdown, preserving a
useful result without changing normal behavior. The existing host title path,
template expansion, and Lua `Far.Title` behavior remain untouched.

### Pass 3: regression and scope

The action test covers both a workspace frame and a modal `User Menu` dialog,
including the asynchronous clipboard write. Existing title-rendering tests
continue to assert that transient menus do not alter the host terminal title.
The change is limited to the debugging action, its user-facing descriptions,
documentation, and regression coverage.
