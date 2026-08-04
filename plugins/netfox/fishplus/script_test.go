package fishplus

import (
	"strings"
	"testing"
)

func TestCompactDropsCommentsAndIndentation(t *testing.T) {
	src := "# a comment\n\n  echo hello\n\t# indented comment\n  \n  case $x in\n   foo ) echo bar;;\n  esac\n"
	want := "echo hello\ncase $x in\nfoo ) echo bar;;\nesac\n"
	if got := Compact(src); got != want {
		t.Errorf("Compact() = %q, want %q", got, want)
	}
}

func TestHelperScriptSubstitutesToken(t *testing.T) {
	const token = "0123456789abcdef"
	script := HelperScript(token)
	if strings.Contains(script, tokenPlaceholder) {
		t.Error("token placeholder survived substitution")
	}
	if !strings.Contains(script, "F4TOKEN="+token) {
		t.Error("session token was not substituted into the helper")
	}
	if strings.Contains(script, "\n#") || strings.HasPrefix(script, "#") {
		t.Error("comments survived compaction")
	}
	for _, needle := range []string{"FISHPLUS", "f4_dec", "ping", "noop"} {
		if !strings.Contains(script, needle) {
			t.Errorf("helper script lost %q during compaction", needle)
		}
	}
}

func TestHelperScriptHasNoHeredocs(t *testing.T) {
	// Compact() strips indentation, which would corrupt here-documents and
	// multi-line literals, so the helper must not contain any.
	if strings.Contains(HelperSource(), "<<") {
		t.Error("helper script uses a here-document, which Compact() would break")
	}
}
