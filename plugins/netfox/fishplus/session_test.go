package fishplus

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockRequest struct {
	ID    string
	Cmd   string
	Args  []string
	Paths []string
}

// decodePaths returns the path lines of a mock request, undoing the tilde
// escape where the client had to fall back to base64.
func (r mockRequest) decodePaths(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(r.Paths))
	for _, line := range r.Paths {
		if !strings.HasPrefix(line, "~") {
			out = append(out, line)
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "~"))
		if err != nil {
			t.Fatalf("path line %q is not valid base64: %v", line, err)
		}
		out = append(out, string(raw))
	}
	return out
}

// newMockPeer returns a session wired to an in-memory peer. The peer greets
// the client with banner (unless empty) and then answers every request via
// handle. Reading and writing happen in separate goroutines, so the client
// may keep writing while the peer is answering.
func newMockPeer(t *testing.T, banner string, handle func(w io.Writer, token string, req mockRequest), pathLines ...int) *Session {
	t.Helper()
	extra := 0
	if len(pathLines) > 0 {
		extra = pathLines[0]
	}
	peerR, cliW := io.Pipe()
	cliR, peerW := io.Pipe()
	sess := NewSession(cliW, cliR, nil)

	reqs := make(chan mockRequest, 32)
	uploaded := make(chan struct{})
	var uploadedOnce sync.Once
	go func() {
		defer close(reqs)
		sc := bufio.NewScanner(peerR)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		scriptDone := false
		for sc.Scan() {
			if !scriptDone {
				if sc.Text() == HelperEndMarker {
					// The helper is in; a real shell would start it now.
					scriptDone = true
					uploadedOnce.Do(func() { close(uploaded) })
				}
				continue
			}
			fields := strings.Fields(sc.Text())
			if len(fields) < 2 {
				continue
			}
			if _, err := strconv.Atoi(fields[0]); err != nil {
				continue // a line of the helper script, not a request
			}
			req := mockRequest{ID: fields[0], Cmd: fields[1], Args: fields[2:]}
			for i := 0; i < extra && sc.Scan(); i++ {
				req.Paths = append(req.Paths, sc.Text())
			}
			reqs <- req
		}
	}()
	go func() {
		if banner != "" {
			fmt.Fprintf(peerW, "Last login: never, this host is a fake\n")
			// The bootstrap announces itself, takes the script, and only
			// then does the helper get to speak.
			fmt.Fprintf(peerW, "%s\n", ReadyMarker(sess.Token()))
			<-uploaded
			fmt.Fprintf(peerW, ".%s 0 %s\n", sess.Token(), banner)
		}
		for req := range reqs {
			if handle != nil {
				handle(peerW, sess.Token(), req)
			}
		}
	}()
	t.Cleanup(func() {
		cliW.Close()
		peerW.Close()
	})
	return sess
}

func TestHandshakeParsesBannerAndSkipsNoise(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1 dd base64 stat", nil)
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	feats := sess.Features()
	if feats.Proto != ProtocolVersion {
		t.Errorf("Proto = %d, want %d", feats.Proto, ProtocolVersion)
	}
	for _, name := range []string{"dd", "base64", "stat"} {
		if !feats.Has(name) {
			t.Errorf("feature %q not announced, raw = %q", name, feats.Raw)
		}
	}
	if feats.Has("find") {
		t.Error("feature \"find\" reported but never announced")
	}
	if got := strings.Join(feats.Names(), " "); got != "base64 dd stat" {
		t.Errorf("Names() = %q, want %q", got, "base64 dd stat")
	}
}

func TestHandshakeRejectsForeignProtocol(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 999 dd", nil)
	err := sess.Handshake(context.Background())
	if err == nil {
		t.Fatal("handshake accepted an unknown protocol version")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error %v does not mention the remote version", err)
	}
	if !sess.Broken() {
		t.Error("session with a foreign protocol must be marked broken")
	}
}

func TestHandshakeReportsRemoteFailure(t *testing.T) {
	sess := newMockPeer(t, "err no base64 decoder found on remote host", nil)
	err := sess.Handshake(context.Background())
	if err == nil {
		t.Fatal("handshake accepted a failing remote")
	}
	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("error %v does not carry the remote message", err)
	}
}

func TestExecCollectsTextLines(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1 stat", func(w io.Writer, token string, req mockRequest) {
		fmt.Fprintf(w, "first\n")
		fmt.Fprintf(w, "#not a data frame in text mode\n")
		fmt.Fprintf(w, "last\n")
		fmt.Fprintf(w, ".%s %s ok\n", token, req.ID)
	})
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	resp, err := sess.Exec(context.Background(), "enum", "/tmp")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !resp.OK() {
		t.Fatalf("status = %q, msg = %q", resp.Status, resp.Msg)
	}
	want := []string{"first", "#not a data frame in text mode", "last"}
	if strings.Join(resp.Lines, "|") != strings.Join(want, "|") {
		t.Errorf("Lines = %q, want %q", resp.Lines, want)
	}
	if len(resp.Data) != 0 {
		t.Errorf("Data = %q, want empty in text mode", resp.Data)
	}
}

func TestExecPathEscapesOnlyWhenNecessary(t *testing.T) {
	var mu sync.Mutex
	var seen []mockRequest
	done := make(chan struct{}, 4)
	sess := newMockPeer(t, "ok FISHPLUS 1", func(w io.Writer, token string, req mockRequest) {
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		fmt.Fprintf(w, ".%s %s ok\n", token, req.ID)
		done <- struct{}{}
	}, 1)
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	// Everything but a newline has to travel raw: base64 costs a fork on
	// the remote host, so it stays a last resort.
	const raw = "/tmp/two words/пример\tтаб\\с чертой "
	const escaped = "/tmp/two\nlines"
	if _, err := sess.ExecPath(context.Background(), "info", raw, "-L"); err != nil {
		t.Fatalf("exec raw: %v", err)
	}
	if _, err := sess.ExecPath(context.Background(), "info", escaped); err != nil {
		t.Fatalf("exec escaped: %v", err)
	}
	<-done
	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("peer saw %d requests, want 2", len(seen))
	}
	if seen[0].Cmd != "info" || len(seen[0].Args) != 1 || seen[0].Args[0] != "-L" {
		t.Errorf("request = %q %q", seen[0].Cmd, seen[0].Args)
	}
	if len(seen[0].Paths) != 1 || seen[0].Paths[0] != raw {
		t.Errorf("path line = %q, want it raw: %q", seen[0].Paths, raw)
	}
	if len(seen[1].Paths) != 1 || !strings.HasPrefix(seen[1].Paths[0], "~") {
		t.Fatalf("path with a newline was not escaped: %q", seen[1].Paths)
	}
	if got := seen[1].decodePaths(t); got[0] != escaped {
		t.Errorf("escaped path decoded to %q, want %q", got[0], escaped)
	}
}

func TestExecRejectsWhitespaceArguments(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1", nil)
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if _, err := sess.Exec(context.Background(), "mode", "two words"); err == nil {
		t.Error("an argument with a space must not be sent as a bare token")
	}
	if _, err := sess.Exec(context.Background(), "mode", ""); err == nil {
		t.Error("an empty argument must be rejected")
	}
}

func TestExecDataReadsBinaryFrames(t *testing.T) {
	payload := []byte{0x00, 0x01, '\n', 0xff, '.', '#', 'x'}
	sess := newMockPeer(t, "ok FISHPLUS 1 dd", func(w io.Writer, token string, req mockRequest) {
		fmt.Fprintf(w, "#%d\n", len(payload))
		w.Write(payload)
		fmt.Fprintf(w, "#%d\n", 3)
		w.Write([]byte("abc"))
		fmt.Fprintf(w, ".%s %s ok\n", token, req.ID)
	})
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	resp, err := sess.ExecData(context.Background(), "read", "/tmp/x", "0", "7")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	want := append(append([]byte{}, payload...), []byte("abc")...)
	if !bytes.Equal(resp.Data, want) {
		t.Errorf("Data = %q, want %q", resp.Data, want)
	}
	if len(resp.Lines) != 0 {
		t.Errorf("Lines = %q, want none", resp.Lines)
	}
}

func TestRemoteErrorIsReported(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1", func(w io.Writer, token string, req mockRequest) {
		fmt.Fprintf(w, ".%s %s err No such file or directory\n", token, req.ID)
	})
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	resp, err := sess.Exec(context.Background(), "stat", "/nope")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if resp.OK() {
		t.Fatal("failing request reported as ok")
	}
	err = resp.Err("stat")
	if err == nil {
		t.Fatal("Err() returned nil for a failed response")
	}
	var remote *RemoteError
	if !errors.As(err, &remote) {
		t.Fatalf("error %v is not a *RemoteError", err)
	}
	if remote.Msg != "No such file or directory" {
		t.Errorf("Msg = %q", remote.Msg)
	}
}

func TestClosedSessionRefusesRequests(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1", nil)
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := sess.Exec(context.Background(), "noop"); err != ErrBroken {
		t.Errorf("Exec after Close = %v, want ErrBroken", err)
	}
}

func TestCancelledContextIsNotSentToRemote(t *testing.T) {
	sess := newMockPeer(t, "ok FISHPLUS 1", nil)
	if err := sess.Handshake(context.Background()); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := sess.Exec(ctx, "noop"); err != context.Canceled {
		t.Errorf("Exec with cancelled context = %v, want context.Canceled", err)
	}
	if sess.Broken() {
		t.Error("session must survive a request that was never sent")
	}
}

// syncBuffer collects the child shell's stderr without racing the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestHelperAgainstLocalShell runs the real helper script in a local POSIX
// shell. It is the only test that proves the script and the Go client agree
// on the wire format.
func TestHelperAgainstLocalShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX shell on Windows")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell available")
	}
	cmd := exec.Command(shell)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start %s: %v", shell, err)
	}
	sess := NewSession(stdin, stdout, stdin)
	t.Cleanup(func() {
		sess.Close()
		done := make(chan struct{})
		go func() {
			cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
		}
	})

	ctx := context.Background()
	if err := sess.Handshake(ctx); err != nil {
		if strings.Contains(err.Error(), "base64") {
			t.Skipf("no base64 on this host: %v", err)
		}
		t.Fatalf("handshake: %v (shell stderr: %s)", err, stderr.String())
	}
	feats := sess.Features()
	if feats.Proto != ProtocolVersion {
		t.Fatalf("Proto = %d, want %d", feats.Proto, ProtocolVersion)
	}
	if !feats.Has("base64") {
		t.Errorf("base64 not announced although the handshake succeeded, raw = %q", feats.Raw)
	}

	if err := sess.Noop(ctx); err != nil {
		t.Errorf("noop: %v", err)
	}

	const payload = "spaces and юникод and 'quotes' and $VARS"
	got, err := sess.Ping(ctx, payload)
	if err != nil {
		t.Fatalf("ping: %v", err)
	}
	if got != payload {
		t.Errorf("ping echoed %q, want %q", got, payload)
	}

	resp, err := sess.Exec(ctx, "feats")
	if err != nil {
		t.Fatalf("feats: %v", err)
	}
	if len(resp.Lines) != 1 || !strings.HasPrefix(resp.Lines[0], strconv.Itoa(ProtocolVersion)) {
		t.Errorf("feats payload = %q", resp.Lines)
	}

	resp, err = sess.Exec(ctx, "frobnicate")
	if err != nil {
		t.Fatalf("unknown command must not break the session: %v", err)
	}
	if resp.OK() || resp.Msg != "unknown command" {
		t.Errorf("unknown command: status = %q, msg = %q", resp.Status, resp.Msg)
	}

	// The session has to stay usable after a rejected command.
	if err := sess.Noop(ctx); err != nil {
		t.Errorf("noop after error: %v", err)
	}
}
