# Issue #491 solution review

## Pass 1 — change the fuzzy matcher

Modify vtui's fuzzy-ranking algorithm so an exact match always wins. Rejected:
the matcher is shared by other tables, and changing its global ranking would
alter unrelated search dialogs.

## Pass 2 — disable fuzzy search in the hotkey dialog

Replace QuickSearch with a plain substring lookup. Rejected: users still need
fuzzy lookup for command labels and descriptions; removing it would regress a
useful part of the configurator.

## Pass 3 — normalize compact key queries, then prefer exact hits

Enable vtui's `SearchExactOnHit` option for the hotkey table and normalize
compact modifier queries such as `ctrla` to the displayed `Ctrl+A` form before
the matcher runs. When an exact key or command match exists, only those rows
remain; when it does not, the normal fuzzy results remain available. Add a
regression test for `ctrla` matching `Ctrl+A` without retaining `Ctrl+Up` and
`Ctrl+PgUp`.

## Safety checks

- the behavior is scoped to the hotkey configurator;
- fuzzy search remains available for non-exact queries;
- the existing table sorting and row-selection fixes remain independent;
- vtui's full-cell exact-hit behavior is supplied by merged PR #89 and consumed
  as published `github.com/unxed/vtui v0.1.285`;
- native Win32 validation exercises the exact-key query in the real dialog and
  reaches the assignment flow after Enter.
