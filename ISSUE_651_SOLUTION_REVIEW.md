# Issue #651 solution review

This review is for the actionable checklist items on current `upstream/main`.
The disputed Panel settings layout is intentionally excluded.

## Candidate solutions

1. **Long menus:** clamp a `vtui.VMenu` created by `MenuBar` to the available
   screen height and rely on its existing `ScrollView`/scrollbar behavior.
2. **Long menus:** replace long menus with a paged menu in f4. Rejected because
   every vtui consumer would retain the same overflow bug.
3. **Long menus:** shorten or hide menu entries. Rejected because it removes
   commands instead of making them reachable.

1. **Hotkey display:** make the single shortcut chosen for a menu item
   deterministic, while keeping all active bindings available to the command
   palette. Add real Far-style archive shortcuts for the two archive actions.
2. **Hotkey display:** render every alternate shortcut in every menu. Rejected
   because it makes narrow menus wider and changes the established single-slot
   menu layout.
3. **Hotkey display:** remove alternate bindings. Rejected because it breaks
   existing navigation behavior.

1. **Autosave:** retain the legacy master switch for compatibility and add
   independent switches for dialog settings, panel/workspace state, current
   panel location/cursor, and GUI geometry. Automatic writes consult the
   relevant switch; explicit Shift+F9 saves remain explicit and unaffected.
2. **Autosave:** add only one new master switch. Rejected because it cannot
   express the four groups requested by the issue.
3. **Autosave:** split settings into unrelated files. Rejected because it
   changes the existing session/config format and complicates compatibility.

## Three review passes

### Pass 1 — scope and compatibility

Current main already has windowed GUI dimensions, Left workspace shortcuts,
the Command palette action, and a selective manual save dialog. The remaining
code defects are the vtui menu geometry, map-order shortcut selection, missing
archive command metadata/handlers, and the single autosave switch.

### Pass 2 — failure modes

Menu clamping must preserve the inclusive terminal coordinate convention and
leave at least one selectable row; vtui already owns `ViewHeight`, `TopPos`,
and scrollbar drawing. Shortcut selection must not remove any binding. New
autosave keys must default from the old `AutoSaveSettings` key so existing
profiles retain behavior. Manual saves must not be blocked by autosave flags.

### Pass 3 — regression risk

The changes will be covered by focused tests for menu viewport bounds,
deterministic alternate-key selection, archive registrations, autosave
round-tripping/gating, and session-field merging. Native Linux ANSI execution
will verify the generated menu remains navigable at a small terminal height;
the GUI window-size behavior will be reported as already present on current
main rather than changed speculatively.

## Decision

Implement the common vtui fix plus the f4-side deterministic shortcut,
archive, and autosave changes. Leave the disputed settings layout and already
working GUI/Command-palette items unchanged.
