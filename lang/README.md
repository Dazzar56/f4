

How to test (in repo root):

GOMAXPROCS=1 go test -run="Test.*(Lang|Layout|Translation|Contamination)" -v

This command must stay green. Two cheap canaries guard the translation files
against machine translation damage:

  TestLanguageAlphabetsContamination
      a script the language may not use at all (Cyrillic inside he.lng).
  TestTranslationsAreFreeOfHomoglyphs
      a single word written in two scripts at once: a Hebrew word carrying an
      Arabic vav, a Belarusian word ending in a Latin "ka". Catches damage
      inside a script the language does use, which the check above cannot see.

Both tests cache their result and skip when nothing they depend on changed.
Set F4_FORCE_TESTS=1 (or CI=1) to run them unconditionally.

