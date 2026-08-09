# Localization work plan

This file is the working state of the localization (i18n/l10n) effort in f4.
It is intentionally self-sufficient: the work can be resumed from this document
plus the repository alone, with no chat history.

`I18N.md` describes how the localization engine works. This file describes what
is broken, what the target state is, and in which order we get there.

## 1. Target state

1. Every language shipped in `lang/` is complete (contains every key of
   `lang/en.lng`) and contains text of that language only.
2. Every language still listed as "planned" in `I18N.md` is shipped.
3. CI fails automatically when somebody adds a hardcoded UI caption, drops a
   translation key, breaks a format placeholder, or makes a dialog overflow in
   some language.

## 2. Ground rules for translators and for models

* `lang/en.lng` is the single source of truth. Keys are never translated, only
  values are.
* If the meaning of an English string changes, rename the key (see `I18N.md`
  section 4). Do not silently reuse a key with a new meaning.
* `&` marks the hotkey letter, `&&` is a literal ampersand. The letter after
  `&` must actually occur in the translated word.
* `\n` is unescaped at load time by `loadLangMapFromINI`. A translation must
  keep the same number of `\n` and exactly the same `%s` / `%d` / `%v`
  placeholders, in the same order, as the English original.
* Keep the key order of `en.lng`. It makes diffs and gap analysis readable.
* Do not shorten English texts just to make a layout fit unless a layout test
  demands it: the English string is what most users see.

## 3. Where we are

Snapshot taken on 2026-08-09 against `lang/en.lng` with 639 keys in
`[Strings]`. Refresh these numbers whenever a stage lands.

### 3.1 Coverage

| language | keys | missing |
| -------- | ---- | ------- |
| ru       | 639  | 0       |
| be       | 639  | 0       |
| zh       | 639  | 0       |
| cs       | 639  | 0       |
| de       | 639  | 0       |
| pl       | 639  | 0       |
| uk       | 639  | 0       |
| ko       | 639  | 0       |
| ja       | 639  | 0       |

### 3.2 Wrong-language contamination (purged)

Whole blocks of one translation had been copied into another file: the German
translation was used as the base for Hungarian, Italian and Dutch, and Dutch was
then partly filled from Hungarian. Measured before the purge, counting values
that were byte-identical to another non-English language and different from the
English original: `nl` 239 keys, `it` 107, `hu` 105. `help/hu.hlf`,
`help/it.hlf` and `help/nl.hlf` were compromised the same way, down to stray
Han characters inside the Hungarian help.

Those six files were deleted rather than repaired. Equality against another
language only finds the copies that were left untouched; the files also
contained half-translated sentences with German verb stems glued into Hungarian
and Italian words, and nothing cheap detects those. Hungarian, Italian and
Dutch are therefore back in the queue of languages to translate from scratch.

The remaining languages were clean apart from German key names that a careless
search and replace had spread everywhere: `Umschalt`, `Entf`, `Einfg` and
`Strg` in `cs`, `pl`, `zh`, `ko`, `uk` (13 lines in total) and one line of
`help/ko.hlf`. Those were fixed in place and are now guarded by
`TestTranslationsAreFreeOfGermanLeftovers`.

What is left of the original problem is a set of harmless coincidences: about
70 values that two languages legitimately share (Russian and Ukrainian share
36, Czech and Polish 10, and every language spells `RAM` and `CPU` the same
way). A general cross-language detector therefore needs an allowlist, which is
why it is part of stage S1 and not of this purge.

### 3.3 Hardcoded UI strings

About 89 captions are still passed to vtui constructors as Go string literals.
Worst offenders: `editor_view.go` (16), `attributes_dialog.go` (14),
`hotkeys_ui.go` (10), `actions.go` (9), `plugring_ui.go` (5),
`user_menu_ui.go` (4), `file_associations_editor.go` (4), `panels_frame.go` (4).
They are frozen in `tools/hardcoded_baseline.txt`; the list may only shrink.

### 3.4 Layout validation failures (fixed)

`TestAllDialogs_LayoutValidation` failed for six dialogs. The purge of stage S2
removed the Hungarian, Italian and Dutch entries, and stage S4 fixed the rest by
shortening the offending translations: `Settings.Appearance` (de, pl),
`Settings.Colorer` (ko), `Settings.Confirmations` (ru), `Settings.Editor`
(de, ru), `Settings.Panel` (de, pl, uk), `Settings.Plugins` (de).

No dialog geometry was changed. Section 7.1 has the arithmetic needed to keep it
that way.

### 3.5 Languages still to add

Hungarian, Italian and Dutch have to be redone from scratch after the purge of
stage S2, help files included. On top of that, `I18N.md` section 6 lists
Spanish, French, Belarusian, Estonian, Latvian, Lithuanian, Danish, Norwegian,
Swedish, Georgian, Armenian and Azerbaijani. Japanese exists but is barely half
translated.

## 4. Stages

The stages are numbered in the order they were designed, not in the order they
are executed. Current execution order: S0 (done), S2 (done), S4 (done), then
**S1 (done)**, S3, S5, S6, S7 at a calmer pace. Section 7 has step by step
recipes for the remaining ones.

### S0 - hardcode gate (DONE)

The AST scanner lives in `tools/hardcode`. `tools/find_hardcoded.go` is a thin
CLI on top of it, and `TestNoNewHardcodedUIStrings` (root package) turns it
into a CI gate: any caption that is not already listed in
`tools/hardcoded_baseline.txt` fails the build.

Notes on the mechanism:

* A baseline entry is `file`, `constructor` and the quoted literal, separated
  by tabs. Line numbers are deliberately not part of it, so ordinary edits do
  not invalidate the baseline.
* Identical entries are de-duplicated, so a second literal copy of an already
  known caption in the same file and the same constructor is not caught. This
  is a conscious trade-off for baseline stability.
* The baseline may only shrink. When a caption is localized, regenerate it with
  `F4_UPDATE_HARDCODED_BASELINE=1 go test -run TestNoNewHardcodedUIStrings .`
  and commit the result; otherwise the test fails as "stale".
* If the file is missing it is created on the next test run and the test
  passes once, so the very first run after this commit produces the list.

### S1 - .lng consistency test (DONE)

Add `lang_consistency_test.go` that walks `lang/*.lng` and fails on:

* duplicate keys inside one file (already present in `pl.lng` before this
  commit, so the test would have caught it);
* keys that do not exist in `en.lng`;
* `[Language]` metadata missing, or `Code` not equal to the file name;
* a different set or order of `%` placeholders compared to English;
* a different number of `\n` escapes compared to English;
* an odd `&` that is not followed by a letter present in the value.

Add a coverage ratchet `lang/coverage_baseline.txt` with lines `code=count`
and fail when a language drops below its recorded number of keys. Update the
file (upwards only) in the same commit that adds translations.

### S2 - contamination purge (DONE)

`lang/hu.lng`, `lang/it.lng`, `lang/nl.lng`, `help/hu.hlf`, `help/it.hlf` and
`help/nl.hlf` were deleted; the German key names left in `cs`, `pl`, `zh`, `ko`,
`uk` and in `help/ko.hlf` were fixed in place. See section 3.2 for the numbers
and the reasoning. Re-translating the three languages is not part of this stage
any more: it moved to the tail of the plan, into S5.

`TestTranslationsAreFreeOfGermanLeftovers` in `lang_contamination_test.go`
keeps the specific accident from happening again. It is a word list, not a
language detector: extend it when a new leak is found rather than trying to
make it clever.

### S3 - fill the gaps

Add the missing keys of 3.1 in `en.lng` order. Order of work: `zh`, `cs`,
`de`, `pl`, `uk` (43 keys or less each), then `hu`, `it`, `nl`, `ko`, and
finally `ja`, which needs a dedicated pass of its own.

### S4 - layout fixes (DONE)

Every failure was a translation that did not fit, so every fix was a shorter
translation; no dialog was widened and no Go file was touched. One Russian typo
was corrected on the way (`конецом` to `концом`), and the German plugin buttons
had to lose eight cells between them for the centred button row to fit.

New translations added in stage S3 must respect the limits in section 7.1. When
a genuinely necessary caption cannot be shortened in some language, widening the
dialog is allowed, but then every language has to be rechecked, which is why it
is the last resort and not the first.

### S5 - new languages

One language per commit, in the order of `I18N.md` section 6. Copy `en.lng`,
translate, set `[Language]` metadata, add the coverage baseline entry, and
remove the language from the "planned" list in `I18N.md`.

### S6 - eliminate the hardcode

Per file, from the top of the 3.3 list: add keys to `en.lng`, replace the
literal with `Msg("...")`, regenerate the baseline, and add the new keys to
every complete translation. When the baseline reaches zero entries, delete it
and change `TestNoNewHardcodedUIStrings` into a plain "no findings at all"
assertion.

### S7 - widen the detector

The scanner currently understands `NewLabel`, `NewButton`, `NewCheckbox`,
`NewCenteredDialog`, `NewVMenu`, `NewText` and the options slice of
`NewComboBox`. Extend it to menu items, input boxes, dialog titles assembled
with `fmt.Sprintf`, and any vtui widget added later.

## 5. Commands

    go run ./tools .                                  # list hardcoded captions
    go test -run TestNoNewHardcodedUIStrings .        # the CI gate
    F4_UPDATE_HARDCODED_BASELINE=1 go test -run TestNoNewHardcodedUIStrings .
    GOMAXPROCS=1 go test -run '^TestAllDialogs_LayoutValidation$' .

## 6. Known issues outside this task

* `TestPTY_Lifecycle` fails with "no space left on device" on a full disk. It is
  an environment problem, not a code problem.
* Dialog titles lost their padding spaces. `en.lng` stores them as
  `FindFile.Title= Find File`, but `ParseIni` trims the value, so the leading
  space never reaches the screen, and several translations no longer have a
  trailing space at all because editors strip trailing whitespace. Fixing it
  means either a parser that preserves values for `.lng` files only, or padding
  the title in the dialog code. It is cosmetic and deliberately deferred.

## 7. Recipes for the executor

### 7.1 Layout arithmetic

The layout validator is the only thing standing between a translation and a
dialog that draws over its own frame. Its messages give both the offending
rectangle and the allowed one, so the fix is always "shorten this caption by
`got.X2 - allowed.X2` cells". These are the rules behind the numbers:

* A dialog of width `W` is centred on the 120x60 test console. Its content lives
  in a VBox at `dlg.X1+2` that is `W-4` wide, so the usable columns are
  `X1+2 .. X1+W-3`.
* An element that ends past `X1+W-3` is reported as touching the frame border,
  one that ends past `X2` as sticking out of the container. Aim for the former
  limit; the latter is already a drawing bug.
* `HBoxLayout` never resizes children horizontally. `AlignFill` only stretches
  them vertically (see `vtui/layout.go`), so a long label pushes a fixed-width
  edit or combo box straight through the border instead of shrinking it.
* Cursor arithmetic inside an HBox: the cursor starts at the box origin, and
  each item consumes `Margins.Left`, its own width, `Margins.Right` and then
  `Spacing` (1 by default).
* Widths: checkbox and button are `4 + len(caption)`, edit and combo box are the
  width passed to the constructor, label and text are `len(caption)`. `&` is not
  printed, `&&` prints one ampersand, and a CJK character occupies two cells.

The three shapes that actually occur:

* Label plus combo or edit of width `N` on one row: `len(label) <= W - 6 - N`.
  Panel settings (`W=60`, `N=24`) allow 30, Appearance (`W=60`, `N=30`) allows
  24, Colorer (`W=74`, `N=44`) allows 24.
* A checkbox on its own row: `len(caption) <= W - 8`. Confirmations (`W=44`)
  allows 36, Panel settings allows 52.
* The two column checkbox grid of the editor settings (`W=78`): the left column
  starts at `X1+2` and must end before the right column at `X1+40`, so
  `len <= 33`; the right column must end inside the frame, so `len <= 32`.
* A centred button row: `sum(4 + len(button)) + 2*(count-1) <= W - 4`. The four
  plugin buttons in a 60 wide dialog therefore allow 34 cells of text in total.

Run only the layout suite while iterating:

    GOMAXPROCS=1 go test -run '^TestAllDialogs_LayoutValidation$' .

### 7.2 Adding missing keys to an existing language (stage S3)

1. Diff the keys of `lang/en.lng` against the target file. Every key of the
   English file must exist, in the same order; there must be no extra keys.
2. Translate value by value. Keep the `%s` / `%d` / `%v` placeholders and the
   `\n` escapes identical to the English original, and keep exactly one `&`
   in front of a letter that occurs in the translated word.
3. Check the result against section 7.1 before running the tests: a caption that
   is more than a few cells longer than the English one is the usual cause of a
   layout failure.
4. Never copy a value from another translation. That is what created the mess
   documented in section 3.2.

### 7.3 Adding a new language (stage S5)

Copy `lang/en.lng` to `lang/<code>.lng`, set `Name`, `Code` and `Author` in the
`[Language]` section, translate the `[Strings]` section, add the display name to
the language menu in `actions.go` (the switch over language codes), remove the
language from the planned list in `I18N.md`, and only then run the tests: the
layout validator checks every shipped language, so a new file can turn six
green dialogs red at once.

### 7.4 Removing a hardcoded caption (stage S6)

1. Run `go run ./tools .` to see the list, and pick one file to work through.
2. Add a key to `lang/en.lng` next to its neighbours from the same dialog, using
   the `Area.Thing` naming of the surrounding keys.
3. Replace the literal in the Go source with `Msg("Area.Thing")`.
4. Regenerate the baseline:
   `F4_UPDATE_HARDCODED_BASELINE=1 go test -run TestNoNewHardcodedUIStrings .`
   and confirm that the file only lost lines.
5. Add the new key to every complete translation, or leave it for a stage S3
   pass; English fallback keeps the UI usable meanwhile.

The same applies to test files, which the scanner deliberately ignores: a test
that compares a UI string with an English literal breaks as soon as somebody
runs the suite in another language. `find_file_test.go` was exactly that bug.