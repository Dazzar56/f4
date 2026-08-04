package fishplus

import (
	_ "embed"
	"strings"
)

// ProtocolVersion is the FISH+ wire protocol revision implemented by both
// this package and the embedded helper script. The remote side reports its
// own version in the handshake banner; a mismatch is fatal for the session.
const ProtocolVersion = 1

// tokenPlaceholder is replaced by a per-session random token before the
// helper is sent to the remote shell.
const tokenPlaceholder = "__F4_TOKEN__"

//go:embed helper.sh
var helperSource string

// HelperSource returns the unmodified helper script. Useful for tests and
// for dumping the script when debugging a remote host.
func HelperSource() string { return helperSource }

// Compact strips comments and blank lines from a shell script and removes
// leading indentation. The helper is sent over the wire on every connect,
// so shaving it down is worth the few lines of code. The helper script must
// therefore not rely on here-documents or multi-line string literals.
func Compact(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		b.WriteString(trimmed)
		b.WriteByte('\n')
	}
	return b.String()
}

// HelperScript returns the compacted helper script with the session token
// substituted, ready to be written into the remote shell's stdin.
func HelperScript(token string) string {
	return Compact(strings.ReplaceAll(helperSource, tokenPlaceholder, token))
}
