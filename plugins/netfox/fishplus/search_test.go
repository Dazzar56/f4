package fishplus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepOptionsMode(t *testing.T) {
	for _, tc := range []struct {
		opts GrepOptions
		want string
	}{
		{GrepOptions{}, "e"},
		{GrepOptions{Fixed: true}, "f"},
		{GrepOptions{IgnoreCase: true}, "ei"},
		{GrepOptions{Fixed: true, IgnoreCase: true}, "fi"},
	} {
		if got := tc.opts.mode(); got != tc.want {
			t.Errorf("mode(%+v) = %q, want %q", tc.opts, got, tc.want)
		}
	}
}

// TestGrepAgainstLocalShell checks the offsets against the ones the same
// content has in memory, because an offset that is off by a line is exactly
// the kind of mistake a parser written against captured output survives.
func TestGrepAgainstLocalShell(t *testing.T) {
	c := newLocalShellClient(t)
	ctx := context.Background()
	if !c.CanGrep() {
		t.Skip("no grep and awk on this host")
	}

	var body strings.Builder
	for i := 0; i < 500; i++ {
		body.WriteString("filler line to push the offsets past a single block\n")
	}
	needleAt := int64(body.Len()) + 6
	body.WriteString("here: NEEDLE and nothing else\n")
	for i := 0; i < 500; i++ {
		body.WriteString("more filler, this time after the interesting line\n")
	}
	lowerAt := int64(body.Len()) + 6
	body.WriteString("here: needle in lower case\n")
	content := body.String()

	file := filepath.Join(t.TempDir(), "a big log.txt")
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := c.Grep(ctx, file, "NEEDLE", GrepOptions{Fixed: true})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if len(got) != 1 || got[0] != needleAt {
		t.Fatalf("offsets = %v, want [%d]", got, needleAt)
	}
	if content[got[0]:got[0]+6] != "NEEDLE" {
		t.Errorf("the offset does not point at the match: %q", content[got[0]:got[0]+6])
	}

	got, err = c.Grep(ctx, file, "needle", GrepOptions{Fixed: true, IgnoreCase: true})
	if err != nil {
		t.Fatalf("grep ignoring case: %v", err)
	}
	if len(got) != 2 || got[0] != needleAt || got[1] != lowerAt {
		t.Fatalf("offsets = %v, want [%d %d]", got, needleAt, lowerAt)
	}

	// A regular expression, and the limit that keeps a match-everything
	// pattern from flooding the session.
	got, err = c.Grep(ctx, file, "^more filler", GrepOptions{})
	if err != nil {
		t.Fatalf("grep with a regexp: %v", err)
	}
	if len(got) != 500 {
		t.Errorf("regexp matches = %d, want 500", len(got))
	}
	got, err = c.Grep(ctx, file, "^more filler", GrepOptions{Limit: 7})
	if err != nil {
		t.Fatalf("grep with a limit: %v", err)
	}
	if len(got) != 7 {
		t.Errorf("limited matches = %d, want 7", len(got))
	}

	// A pattern with spaces, which only survives because it travels on a
	// line of its own rather than as a request argument.
	got, err = c.Grep(ctx, file, "push the offsets", GrepOptions{Fixed: true})
	if err != nil {
		t.Fatalf("grep with spaces: %v", err)
	}
	if len(got) != 500 {
		t.Errorf("matches for a phrase = %d, want 500", len(got))
	}

	if got, err = c.Grep(ctx, file, "this string is not there", GrepOptions{Fixed: true}); err != nil {
		t.Errorf("grep without matches: %v", err)
	} else if len(got) != 0 {
		t.Errorf("offsets = %v, want none", got)
	}

	if _, err := c.Grep(ctx, file, "", GrepOptions{}); err == nil {
		t.Error("an empty pattern was accepted")
	}
	if _, err := c.Grep(ctx, filepath.Dir(file), "x", GrepOptions{}); err == nil {
		t.Error("a directory was searched")
	}
	if _, err := c.Grep(ctx, file+".missing", "x", GrepOptions{}); err == nil {
		t.Error("a missing file was searched")
	}
	if got, err := c.Session().Ping(ctx, "alive"); err != nil || got != "alive" {
		t.Fatalf("session out of sync after grep: %q %v", got, err)
	}
}
