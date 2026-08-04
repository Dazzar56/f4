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
	if _, err := io.WriteString(s.w, HelperScript(s.token)); err != nil {
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

// Exec runs a command whose payload is text. Arguments are base64 encoded,
// so paths may contain spaces and any other byte except NUL.
func (s *Session) Exec(ctx context.Context, cmd string, args ...string) (*Response, error) {
	return s.exec(ctx, false, cmd, args...)
}

// ExecData runs a command whose payload may contain binary frames. A frame
// is a line "#<n>" followed by exactly n raw bytes.
func (s *Session) ExecData(ctx context.Context, cmd string, args ...string) (*Response, error) {
	return s.exec(ctx, true, cmd, args...)
}

func (s *Session) exec(ctx context.Context, binary bool, cmd string, args ...string) (*Response, error) {
	if strings.ContainsAny(cmd, " \t\r\n") || cmd == "" {
		return nil, fmt.Errorf("fishplus: invalid command %q", cmd)
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
		req.WriteString(base64.StdEncoding.EncodeToString([]byte(arg)))
	}
	req.WriteByte('\n')
	if _, err := io.WriteString(s.w, req.String()); err != nil {
		s.broken = true
		return nil, err
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
			if convErr != nil || n < 0 {
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
	resp, err := s.Exec(ctx, "ping", payload)
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
