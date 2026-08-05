package fishplus

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// DefaultGrepLimit caps how many matches one request brings back. A search
// across a large log can match millions of times, and nobody scrolls through
// millions of hits; the caller asks again with a narrower pattern instead.
const DefaultGrepLimit = 10000

// ErrNoGrep is returned when the remote host lacks the tools the search is
// built from. The caller is expected to fall back to reading the file.
var ErrNoGrep = errors.New("fishplus: the remote host cannot search files")

// GrepOptions selects how the pattern is interpreted and how much comes back.
type GrepOptions struct {
	// Fixed treats the pattern as a plain string rather than as an extended
	// regular expression.
	Fixed bool
	// IgnoreCase folds case the way the remote grep does it, which for a
	// non-ASCII pattern is the remote locale's idea of case.
	IgnoreCase bool
	// Limit caps the number of matches; zero means DefaultGrepLimit.
	Limit int
}

func (o GrepOptions) mode() string {
	m := "e"
	if o.Fixed {
		m = "f"
	}
	if o.IgnoreCase {
		m += "i"
	}
	return m
}

// CanGrep reports whether the remote host announced the tools the search
// needs. A host without them is not an error, it is a fallback to reading.
func (c *Client) CanGrep() bool {
	feats := c.sess.Features()
	return feats.Has("grep") && feats.Has("awk")
}

// Grep runs the search on the remote host and returns the byte offset of
// every match, in the order the file has them. Only the offsets travel: the
// matched text stays where it is, which is the whole reason for searching
// remotely instead of downloading the file.
func (c *Client) Grep(ctx context.Context, p, pattern string, opts GrepOptions) ([]int64, error) {
	if pattern == "" {
		return nil, errors.New("fishplus: empty search pattern")
	}
	if !c.CanGrep() {
		return nil, ErrNoGrep
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultGrepLimit
	}
	resp, err := c.sess.ExecPaths(ctx, "grep", []string{pattern, p}, opts.mode(), strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	if err := resp.Err("grep " + p); err != nil {
		return nil, err
	}
	offsets := make([]int64, 0, len(resp.Lines))
	for _, line := range resp.Lines {
		if line == "" {
			continue
		}
		off, convErr := strconv.ParseInt(line, 10, 64)
		if convErr != nil {
			// A diagnostic line from a remote tool must not cost the caller
			// the matches that did parse.
			continue
		}
		offsets = append(offsets, off)
	}
	if len(offsets) > limit {
		return nil, fmt.Errorf("fishplus: grep %q returned %d offsets for a limit of %d", p, len(offsets), limit)
	}
	return offsets, nil
}
