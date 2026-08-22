package main

// commandHasUnmatchedQuote reports whether command leaves a shell quote open.
// The command line is a single-shot input field, so it cannot provide the
// continuation prompt that an interactive shell would normally use; sending
// such input would make f4 appear to hang while the child shell waits for the
// closing quote.
func commandHasUnmatchedQuote(command string, windowsShell bool) bool {
	var quote rune
	escaped := false
	for _, ch := range command {
		if escaped {
			escaped = false
			continue
		}
		if ch == '^' && windowsShell && quote != '\'' {
			escaped = true
			continue
		}
		if ch == '\\' && !windowsShell && quote != '\'' {
			escaped = true
			continue
		}
		if quote == 0 {
			if ch == '"' || (!windowsShell && ch == '\'') {
				quote = ch
			}
			continue
		}
		if ch == quote {
			quote = 0
		}
	}
	return quote != 0
}
