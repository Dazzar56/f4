# The `vtui` Navigation & Interaction Philosophy

## Abstract

This document outlines the core principles of user experience (UX) and navigation that govern the `vtui` framework and its primary implementation, the `f4` file manager. Our philosophy is rooted in blending the speed and power of classic keyboard-driven interfaces like **Far Manager** with the structured, predictable component model of **Turbo Vision**.

The goal is to create an environment that is instantly familiar to veterans of TUI applications while remaining discoverable and consistent for new users. We achieve this by ensuring that every navigation key has a clear, context-dependent purpose, maximizing user efficiency and respecting "muscle memory".

### 1. The Three Tiers of Navigation

Every interactive screen in `vtui` adheres to a three-tiered navigation hierarchy. This ensures there is always a way to move focus, from the most reliable method to the most context-specific.

#### Tier 1: The Reliable Cycle (`Tab` / `Shift+Tab`)

This is the most fundamental and predictable way to navigate.

*   **Rule:** `Tab` always moves focus to the *next* focusable element in the logical order. `Shift+Tab` always moves to the *previous* one.
*   **Behavior:** This cycle is typically wrapped. Pressing `Tab` on the last element moves focus to the first, and `Shift+Tab` on the first moves to the last.
*   **Purpose:** Guarantees that a user can always reach any interactive element on the screen, regardless of its visual layout. It is the bedrock of accessibility and predictability.

#### Tier 2: The Contextual Jump (Arrow Keys)

Arrow keys provide fast, spatial navigation. Their behavior is highly dependent on the currently focused widget.

*   **Rule:** Arrow keys are primarily used for *internal navigation* within a component (e.g., moving up/down a list, left/right in a text field).
*   **Boundary Behavior:** Arrow keys transfer focus to a neighboring component **only** when the cursor is at the absolute boundary of the current component's data set.
    *   Pressing `Up` or `Left` on the very first item of a list, table, or group will exit focus to the previous element in the `Tab` cycle.
    *   Pressing `Down` or `Right` on the very last item will exit focus to the next element.
    *   In all other cases (e.g., pressing `Up` in the middle of a list), the event is "swallowed" by the component.
*   **Purpose:** This creates an intuitive flow. The user stays "inside" a widget while working with its data, but can seamlessly "flow" to the next widget by continuing to press the arrow key after reaching the end.

#### Tier 3: The Direct Shortcut (Hotkeys)

Hotkeys provide the fastest way to activate a specific function.

*   **Rule:** Hotkeys are typically activated with `Alt+<char>`. An ampersand (`&`) in a label (e.g., "`&Save`") defines the hotkey.
*   **Modeless Activation:** In dialogs where no text input field is focused, hotkeys may be activated directly by pressing the character key without `Alt`.
*   **Purpose:** To provide expert users with immediate access to any action on the screen, bypassing the `Tab` cycle entirely.

### 2. Component-Specific Interaction Patterns

#### Dialogs and Windows

*   `Enter`: Triggers the "default" action. This is either the button marked as `IsDefault`, or the first actionable button in the tab order if none is marked. This applies even if an `Edit` field is focused.
*   `Esc`: Closes the window or dialog.
*   `F1`: Opens the help topic associated with the currently focused element.
*   **Mouse:** Click-and-drag on the top border moves the window. Click-and-drag on the bottom-right corner resizes it.

#### Groups (`RadioGroup`, `CheckGroup`)

*   **Interaction Model:** These components separate the concepts of *cursor* and *selection*.
    *   **Arrow Keys** move the internal cursor/focus within the group. The selection itself does not change.
    *   `Space` or `Enter` toggles the state (checked/unchecked, selected radio button) of the item under the cursor.
*   **Snake Navigation:** In multi-column layouts, navigation is two-dimensional and wraps intuitively:
    *   Pressing `Right` at the end of a row moves the cursor to the beginning of the next row.
    *   Pressing `Down` at the bottom of a column moves the cursor to the top of the next column. `Up` at the top of a column moves to the bottom of the previous.
*   **Rationale:** This model is vastly more efficient for keyboard users than the "arrows change selection" model, as it allows rapid navigation across many options without triggering an action on every key press.

#### Lists (`Table`, `ListBox`)

*   **Navigation:** `Up`/`Down`, `PgUp`/`PgDn`, `Home`/`End` provide standard list navigation.
*   **Boundary Behavior:** As per the core principle, `Up` on the first item and `Down` on the last item will pass focus to the previous/next widget in the dialog.
*   **Action:** `Enter` or `Double-Click` triggers the primary action for the selected item (e.g., opening a file, confirming a choice).
*   **`ListBox` as a `Table`:** `ListBox` is implemented as a single-column `Table` and inherits all its navigation behaviors, ensuring consistency.

#### File Panels (`f4` Specific)

File panels are a special, highly optimized version of a `Table`.

*   `Up`/`Down`, `PgUp`/`PgDn`: Navigate vertically within the current column.
*   `Left`/`Right`: Jump one full page (view height) up or down within the *current column*. If at the top/bottom, jump to the top/bottom of the adjacent column. **These keys do not change the active panel.**
*   `Enter`: Enters a directory or executes a file.
*   `Ctrl+Enter`: Inserts the selected filename into the command line.

#### Menus (`MenuBar`, `VMenu`)

*   **`MenuBar` (Top-level):**
    *   Activated by `F9` or `Alt+<char>`.
    *   When active, `Left`/`Right` cycles through the main menu items (`File`, `Edit`, etc.), automatically opening their respective submenus.
    *   `Down` or `Enter` opens the submenu for the currently selected item.
    *   `Esc` deactivates the menu bar.
*   **`VMenu` (Vertical/Submenu):**
    *   **As a Submenu:** If opened from a `MenuBar`, `Left`/`Right` closes the current submenu and opens the adjacent one from the `MenuBar`. `Up` on the first item or `Down` on the last item wraps around within the `VMenu`.
    *   **As a Standalone Dialog:** If opened as a context menu (not tied to a `MenuBar`), its boundary behavior follows the standard `Tier 2` rule: `Up` on the first item or `Down` on the last will pass focus to the previous/next element in the parent dialog.
*   **Rationale:** This dual behavior makes menus feel integrated when part of a larger structure, but behave like any other standard list widget when used for context-specific actions.

### 3. Mouse Interaction Principles

While `vtui` is keyboard-first, mouse interaction is designed to be consistent and predictable.

*   **Left Click:** Focuses and/or activates an element.
*   **Double-Click:** The primary "action" command, equivalent to `Enter`.
*   **Right Click (Contextual):** In specific components like file panels, right-click can be used for secondary actions like multi-selection.
*   **Wheel:** Scrolls the component under the cursor, regardless of focus.

By adhering to these rules, we aim to build TUI applications that are powerful, efficient, and a pleasure to use for both novice and expert users.