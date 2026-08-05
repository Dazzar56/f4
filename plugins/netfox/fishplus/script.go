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

// HelperEndMarker is the line that follows the helper script on the wire.
// It cannot occur inside the script, which is ours to write.
const HelperEndMarker = "F4EOF"

// ReadyMarker is what the bootstrap prints once the remote shell is done
// parsing it and has started executing. The token is part of it so that
// login noise cannot be mistaken for it.
func ReadyMarker(token string) string { return "F4RDY" + token }

// BootstrapLine is the single line that has to reach the remote shell
// before the helper does.
//
// A shell reads its script from the same stream the requests arrive on, and
// it does not read it a byte at a time: dash fills a buffer, and whatever
// lands in that buffer past the end of the script is parsed as part of it.
// A request that arrives while the shell is still parsing therefore gets
// executed as a shell command — "1: not found" — and the session hangs
// waiting for an answer that will never come. bash happens to read byte by
// byte and never showed it, which is why this survived so long.
//
// So the script is not parsed off the stream at all. This one line is, and
// then it takes the script in through the shell's own read builtin, which
// reads from the file descriptor and cannot run ahead of itself. The marker
// tells the client when the line has been parsed and is running, so that
// nothing is in flight while the parser is still working.
//
// The marker is printed as two pieces on purpose: a terminal that echoes
// the line back would otherwise send the client its own request to match.
func BootstrapLine(token string) string {
	return "echo F4R\"DY\"" + token +
		"; F4NL=$(printf '\\nx'); F4NL=${F4NL%x}; F4S=; " +
		"while IFS= read -r F4L; do [ \"$F4L\" = " + HelperEndMarker + " ] && break; " +
		"F4S=$F4S$F4L$F4NL; done; eval \"$F4S\"\n"
}

// HelperScript returns the compacted helper script with the session token
// substituted, ready to be written into the remote shell's stdin.
func HelperScript(token string) string {
	return Compact(strings.ReplaceAll(helperSource, tokenPlaceholder, token))
}
