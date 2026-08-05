package fishplus

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// MaxLineLen caps a single protocol line. Payload lines are produced by
// remote tools (ls, stat, grep), so they are bounded by path and match
// lengths; anything bigger means the stream went out of sync.
const MaxLineLen = 1 << 20

// MaxFrameLen caps a single binary frame. The client never asks for more
// than this, so a bigger frame means either a confused helper or a hostile
// host trying to make the panel allocate a disk worth of memory.
const MaxFrameLen = 64 << 20

// ErrBroken is returned once a session lost synchronization with the remote
// helper. Such a session cannot be repaired, the caller has to reconnect.
var ErrBroken = errors.New("fishplus: session is out of sync")

// RemoteError carries a failure reported by the remote helper itself.
type RemoteError struct {
	Cmd string
	Msg string
}

func (e *RemoteError) Error() string {
	if e.Cmd == "" {
		return "fishplus: remote error: " + e.Msg
	}
	return "fishplus: " + e.Cmd + ": " + e.Msg
}

// Features describes what the remote host is capable of, as detected by the
// helper script during startup.
type Features struct {
	Proto int
	Raw   string
	names map[string]bool
}

// Has reports whether the remote helper announced the named feature.
func (f Features) Has(name string) bool { return f.names[name] }

// Names returns the announced feature names in a stable order.
func (f Features) Names() []string {
	out := make([]string, 0, len(f.names))
	for name := range f.names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ListingMode returns the metadata backend the helper picked for itself,
// announced as "mode:<name>" among the features.
func (f Features) ListingMode() string {
	for name := range f.names {
		if strings.HasPrefix(name, "mode:") {
			return strings.TrimPrefix(name, "mode:")
		}
	}
	return ""
}

// Response is the outcome of a single request.
type Response struct {
	// Lines holds the textual payload, newlines stripped.
	Lines []string
	// Data holds the concatenated binary payload of a data request.
	Data []byte
	// Status is either "ok" or "err".
	Status string
	// Msg is the optional message that follows the status.
	Msg string
}

// OK reports whether the remote helper completed the request successfully.
func (r *Response) OK() bool { return r.Status == "ok" }

// Err converts a failed response into an error, cmd is used for context.
func (r *Response) Err(cmd string) error {
	if r.OK() {
		return nil
	}
	return &RemoteError{Cmd: cmd, Msg: r.Msg}
}

// Session speaks the FISH+ protocol over a duplex byte stream, typically the
// stdin/stdout pair of a remote shell started through ssh. All requests are
// serialized: the protocol is strictly request/response over one stream.
type Session struct {
	mu     sync.Mutex
	w      io.Writer
	r      *bufio.Reader
	closer io.Closer
	token  string
	seq    uint64
	feats  Features
	broken bool
	closed bool
}

// NewSession wires a session to the remote shell's stdin and stdout. closer
// may be nil; when set it is closed by Close, which also makes the remote
// helper exit because its stdin hits EOF.
func NewSession(stdin io.Writer, stdout io.Reader, closer io.Closer) *Session {
	return &Session{
		w:      stdin,
		r:      bufio.NewReaderSize(stdout, 64*1024),
		closer: closer,
		token:  newToken(),
	}
}

func newToken() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Rather than failing the connection, fall back to a fixed token:
		// it only protects against accidental collisions with payload.
		return "f4f1shplusf4f1"
	}
	return hex.EncodeToString(buf[:])
}

// Token returns the random terminator token of this session.
func (s *Session) Token() string { return s.token }

// Features returns what the remote helper announced during the handshake.
func (s *Session) Features() Features {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.feats
}

// Broken reports whether the session lost synchronization.
func (s *Session) Broken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.broken
}

// Handshake uploads the helper script and waits for its banner. Everything
// the remote shell printed before the banner (motd, shell warnings) is
// discarded.
func (s *Session) Handshake(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return ErrBroken
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := io.WriteString(s.w, BootstrapLine(s.token)); err != nil {
		s.broken = true
		return err
	}
	if err := s.waitForReady(ctx); err != nil {
		return err
	}
	if _, err := io.WriteString(s.w, HelperScript(s.token)+HelperEndMarker+"\n"); err != nil {
		s.broken = true
		return err
	}
	resp, err := s.readResponse(ctx, 0, false)
	if err != nil {
		return err
	}
	if !resp.OK() {
		s.broken = true
		return &RemoteError{Cmd: "handshake", Msg: resp.Msg}
	}
	feats, err := parseBanner(resp.Msg)
	if err != nil {
		s.broken = true
		return err
	}
	if feats.Proto != ProtocolVersion {
		s.broken = true
		return fmt.Errorf("fishplus: remote speaks protocol %d, expected %d", feats.Proto, ProtocolVersion)
	}
	s.feats = feats
	return nil
}

func parseBanner(msg string) (Features, error) {
	fields := strings.Fields(msg)
	if len(fields) < 2 || fields[0] != "FISHPLUS" {
		return Features{}, fmt.Errorf("fishplus: unexpected banner %q", msg)
	}
	proto, err := strconv.Atoi(fields[1])
	if err != nil {
		return Features{}, fmt.Errorf("fishplus: unexpected protocol version %q", fields[1])
	}
	feats := Features{
		Proto: proto,
		Raw:   strings.Join(fields[2:], " "),
		names: make(map[string]bool, len(fields)),
	}
	for _, name := range fields[2:] {
		feats.names[name] = true
	}
	return feats, nil
}

// maxBootstrapLines bounds how much login noise is skipped while waiting
// for the shell to report itself ready. A motd is long; it is not endless.
const maxBootstrapLines = 1000

// waitForReady consumes whatever the login printed until the bootstrap says
// it is running. Sending the helper before that is what the whole two step
// upload exists to avoid.
func (s *Session) waitForReady(ctx context.Context) error {
	marker := ReadyMarker(s.token)
	for i := 0; i < maxBootstrapLines; i++ {
		if err := ctx.Err(); err != nil {
			s.broken = true
			return err
		}
		line, err := s.readLine()
		if err != nil {
			s.broken = true
			return err
		}
		if strings.Contains(line, marker) {
			return nil
		}
	}
	s.broken = true
	return fmt.Errorf("fishplus: the remote shell never reported being ready")
}

// Exec runs a command that takes only short tokens as arguments. A token
// must be non-empty and free of whitespace; anything path shaped belongs in
// ExecPath instead.
func (s *Session) Exec(ctx context.Context, cmd string, args ...string) (*Response, error) {
	return s.exec(ctx, false, cmd, args, nil)
}

// ExecPath runs a command that operates on a path. The path travels on a
// line of its own, verbatim whenever the channel can carry it: only a path
// containing a newline (or starting with the escape marker) is base64
// encoded. Staying out of base64 keeps a fork per request off the remote
// host and keeps the traffic readable in a protocol log.
func (s *Session) ExecPath(ctx context.Context, cmd, path string, args ...string) (*Response, error) {
	return s.exec(ctx, false, cmd, args, []string{path})
}

// ExecPaths runs a command that operates on more than one path, each on a
// line of its own and in the order given. Rename is the first such command.
func (s *Session) ExecPaths(ctx context.Context, cmd string, paths []string, args ...string) (*Response, error) {
	return s.exec(ctx, false, cmd, args, paths)
}

// ExecData and ExecPathData behave like Exec and ExecPath but also accept
// binary frames: a line "#<n>" followed by exactly n raw bytes.
func (s *Session) ExecData(ctx context.Context, cmd string, args ...string) (*Response, error) {
	return s.exec(ctx, true, cmd, args, nil)
}

func (s *Session) ExecPathData(ctx context.Context, cmd, path string, args ...string) (*Response, error) {
	return s.exec(ctx, true, cmd, args, []string{path})
}

// EncodePathLine renders a path as one protocol line, escaping it only when
// a raw line would not survive the round trip.
func EncodePathLine(p string) string {
	if p == "" || strings.HasPrefix(p, "~") || strings.ContainsAny(p, "\r\n") {
		return "~" + base64.StdEncoding.EncodeToString([]byte(p))
	}
	return p
}

// ExecPayload runs a command that carries a payload of its own after the
// path lines. A raw payload is exactly the announced number of bytes with
// nothing around it; an encoded one is a single base64 line, which the
// remote helper can consume with the shell alone and which therefore stays
// exact on hosts whose dd cannot stop on a byte boundary.
func (s *Session) ExecPayload(ctx context.Context, cmd string, paths, args []string, payload []byte, encoded bool) (*Response, error) {
	return s.execFull(ctx, false, cmd, args, paths, payload, encoded, nil)
}

// ExecStream runs a command whose request body the caller writes itself,
// after the path lines and before the answer is read. A command whose
// payload is interleaved with descriptions of it cannot be handed over as
// one slice, which is what patch needs.
func (s *Session) ExecStream(ctx context.Context, cmd string, paths, args []string, body func(w io.Writer) error) (*Response, error) {
	return s.execFull(ctx, false, cmd, args, paths, nil, false, body)
}

// MarkBroken poisons the session after the caller found out, by means the
// session itself cannot see, that the two sides disagree about how much of
// the stream has been consumed.
func (s *Session) MarkBroken() {
	s.mu.Lock()
	s.broken = true
	s.mu.Unlock()
}

func (s *Session) exec(ctx context.Context, binary bool, cmd string, args, paths []string) (*Response, error) {
	return s.execFull(ctx, binary, cmd, args, paths, nil, false, nil)
}

func (s *Session) execFull(ctx context.Context, binary bool, cmd string, args, paths []string, payload []byte, encoded bool, body func(w io.Writer) error) (*Response, error) {
	if cmd == "" || strings.ContainsAny(cmd, " \t\r\n") {
		return nil, fmt.Errorf("fishplus: invalid command %q", cmd)
	}
	for _, arg := range args {
		if arg == "" || strings.ContainsAny(arg, " \t\r\n") {
			return nil, fmt.Errorf("fishplus: invalid argument %q for command %q", arg, cmd)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.broken {
		return nil, ErrBroken
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.seq++
	id := s.seq
	var req strings.Builder
	req.WriteString(strconv.FormatUint(id, 10))
	req.WriteByte(' ')
	req.WriteString(cmd)
	for _, arg := range args {
		req.WriteByte(' ')
		req.WriteString(arg)
	}
	req.WriteByte('\n')
	for _, p := range paths {
		req.WriteString(EncodePathLine(p))
		req.WriteByte('\n')
	}
	if encoded {
		// The line is written even for an empty payload: the helper reads
		// one line per encoded request and would otherwise wait for a line
		// that never comes.
		req.WriteString(base64.StdEncoding.EncodeToString(payload))
		req.WriteByte('\n')
	}
	if _, err := io.WriteString(s.w, req.String()); err != nil {
		s.broken = true
		return nil, err
	}
	if !encoded && len(payload) > 0 {
		// The raw payload carries no terminator of its own: the remote
		// helper reads exactly as many bytes as the request announced, so
		// a stray newline here would end up at the head of the next
		// request.
		if _, err := s.w.Write(payload); err != nil {
			s.broken = true
			return nil, err
		}
	}
	if body != nil {
		// A body that stops halfway leaves the remote host waiting for
		// bytes that will never come, so there is no recovering the stream.
		if err := body(s.w); err != nil {
			s.broken = true
			return nil, err
		}
	}
	return s.readResponse(ctx, id, binary)
}

func (s *Session) readResponse(ctx context.Context, id uint64, binary bool) (*Response, error) {
	prefix := "." + s.token + " " + strconv.FormatUint(id, 10) + " "
	resp := &Response{}
	for {
		if err := ctx.Err(); err != nil {
			// The response is only half-read, so the stream is unusable.
			s.broken = true
			return nil, err
		}
		line, err := s.readLine()
		if err != nil {
			s.broken = true
			return nil, err
		}
		// The handshake is the one place where the terminator may not start
		// its line: a motd, a shell warning or the echo of the uploaded
		// script on a pseudo terminal can end without a newline and glue
		// itself to the banner. Later responses are strict, the helper
		// controls every byte by then.
		if id == 0 {
			if at := strings.Index(line, prefix); at > 0 {
				line = line[at:]
			}
		}
		if strings.HasPrefix(line, prefix) {
			status, msg, _ := strings.Cut(strings.TrimSpace(line[len(prefix):]), " ")
			if status != "ok" && status != "err" {
				s.broken = true
				return nil, fmt.Errorf("fishplus: bad terminator %q", line)
			}
			resp.Status = status
			resp.Msg = strings.TrimSpace(msg)
			return resp, nil
		}
		if id == 0 {
			// Pre-handshake noise, drop it.
			continue
		}
		if binary && strings.HasPrefix(line, "#") {
			n, convErr := strconv.Atoi(line[1:])
			if convErr != nil || n < 0 || n > MaxFrameLen {
				s.broken = true
				return nil, fmt.Errorf("fishplus: bad data frame header %q", line)
			}
			buf := make([]byte, n)
			if _, err := io.ReadFull(s.r, buf); err != nil {
				s.broken = true
				return nil, err
			}
			resp.Data = append(resp.Data, buf...)
			continue
		}
		resp.Lines = append(resp.Lines, line)
	}
}

func (s *Session) readLine() (string, error) {
	var buf []byte
	for {
		chunk, err := s.r.ReadSlice('\n')
		if len(buf)+len(chunk) > MaxLineLen {
			return "", fmt.Errorf("fishplus: response line exceeds %d bytes", MaxLineLen)
		}
		buf = append(buf, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	return strings.TrimRight(string(buf), "\r\n"), nil
}

// Ping asks the remote helper to echo the payload back. It doubles as a
// keepalive and as a synchronization check.
func (s *Session) Ping(ctx context.Context, payload string) (string, error) {
	resp, err := s.ExecPath(ctx, "ping", payload)
	if err != nil {
		return "", err
	}
	if err := resp.Err("ping"); err != nil {
		return "", err
	}
	return strings.Join(resp.Lines, "\n"), nil
}

// Noop is the cheapest possible round trip.
func (s *Session) Noop(ctx context.Context) error {
	resp, err := s.Exec(ctx, "noop")
	if err != nil {
		return err
	}
	return resp.Err("noop")
}

// Close tears the session down. The remote helper terminates on its own once
// its stdin reaches EOF, so no farewell command is sent: a stuck remote must
// never be able to block the UI thread inside Close.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.broken = true
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}
