

How to test (in repo root):

GOMAXPROCS=1 go test -run="Test.*(Lang|Layout|Translation|Contamination|Bidi)" -v

This command must stay green. Three cheap canaries guard the translation files
against machine translation damage:

  TestLanguageAlphabetsContamination
      a script the language may not use at all (Cyrillic inside he.lng).
  TestTranslationsAreFreeOfHomoglyphs
      a single word written in two scripts at once: a Hebrew word carrying an
      Arabic vav, a Belarusian word ending in a Latin "ka". Catches damage
      inside a script the language does use, which the check above cannot see.
  TestTranslationsHaveNoBidiControls
      an invisible LRM/RLM/isolate baked into a string. Ordering belongs to
      the renderer, not to the data.

All three cache their result and skip when nothing they depend on changed.
Set F4_FORCE_TESTS=1 (or CI=1) to run them unconditionally.

What is left of the localization audit, in the order it should be done:

  1. Repair the lines listed in lang/homoglyph_baseline.txt, deleting each
     entry as it is fixed. Start with help/he.hlf, which holds 17 of the 39.
     Entry format is <path>:<line>; the header of that file explains the two
     kinds of damage and the fix for each.
  2. Widen TestTranslationsHaveNoBidiControls from lang/*.lng to help/*.hlf
     in the same pass, once those files have been read at all.
  3. Delete lang/homoglyph_baseline.txt when it is empty; the homoglyph check
     then guards everything with no exceptions.
  4. Only then work off the "Tech Debt -> Missing key" list that
     TestLangConsistency prints. Those keys fall back to English at runtime,
     so they are the least urgent part. The longest lists are hi, hy, hu, zh
     and ar.

Damage of the kind these canaries catch was introduced by translating with a
model and reviewing by eye. Any new translation pass should run the command
above before the commit, not after.

