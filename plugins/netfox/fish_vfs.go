package netfox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/unxed/f4/plugins/netfox/fishplus"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

// fishReadDirChunk is how many entries are handed to the panel at once,
// matching what the SFTP backend does.
const fishReadDirChunk = 500

// FishVFS exposes a FISH+ session as an f4 file system. It owns the session
// and closes it with itself; the transport underneath is whatever stream
// the caller handed over, which is what lets the tests run it on a local
// shell instead of on ssh.
type FishVFS struct {
	parent vfs.VFS
	client *fishplus.Client
	conn   *fishConn
	path   string
	title  string
	once   sync.Once
}

// fishConn keeps one FISH+ session alive for as long as any of the VFS
// instances built on it is still in use. f4 clones a panel's file system in
// several places — the "other panel" menu item and the frame snapshot taken
// for a background task — and every panel closes its own file system when it
// leaves it. Handing back the same instance, the way the SFTP backend does,
// would therefore let one panel tear the session down under another, and
// would make both panels share a single current directory.
//
// Requests from the clones interleave freely: the session serializes them
// with its own mutex, so each request stays atomic. They do not run in
// parallel, which for a shell that answers one command at a time is what a
// second connection could not fix anyway.
type fishConn struct {
	client *fishplus.Client

	mu     sync.Mutex
	refs   int
	closed bool
}

func (c *fishConn) retain() {
	c.mu.Lock()
	c.refs++
	c.mu.Unlock()
}

func (c *fishConn) release() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refs--
	if c.refs > 0 || c.closed {
		return nil
	}
	c.closed = true
	return c.client.Session().Close()
}

// NewFishVFSOnStream completes the handshake on an already established pair
// of streams and opens the panel in whatever directory the remote shell
// started in. closer may be nil; when set it is closed together with the
// session, which is also what makes the remote helper exit.
func NewFishVFSOnStream(ctx context.Context, parent vfs.VFS, stdin io.Writer, stdout io.Reader, closer io.Closer, title string) (*FishVFS, error) {
	sess := fishplus.NewSession(stdin, stdout, closer)
	if err := sess.Handshake(ctx); err != nil {
		sess.Close()
		return nil, err
	}
	client := fishplus.NewClient(sess)
	cwd, err := client.Pwd(ctx)
	if err != nil || !path.IsAbs(cwd) {
		cwd = "/"
	}
	return &FishVFS{
		parent: parent,
		client: client,
		conn:   &fishConn{client: client, refs: 1},
		path:   cwd,
		title:  title,
	}, nil
}

// sshShell ties the lifetime of the remote shell and of the connection that
// carries it to the session that speaks through them.
type sshShell struct {
	sess   *ssh.Session
	client *ssh.Client
}

func (s *sshShell) Close() error {
	s.sess.Close()
	return s.client.Close()
}

// NewFishVFS opens a site over SSH. The shell deliberately runs without a
// pseudo terminal: a terminal would echo every request back, turn each \n of
// a binary frame into \r\n and cut long request lines at the canonical
// buffer limit. The helper can tame a terminal with stty when it has to, but
// not asking for one in the first place is cheaper and cannot fail.
//
// The command is "exec /bin/sh" rather than a plain shell request, because
// the account's login shell may well be csh, fish or something else that
// does not speak the POSIX syntax the helper is written in.
func NewFishVFS(parent vfs.VFS, host, port, user, pass string, timeout int) (*FishVFS, error) {
	vtui.DebugLog("NET: Initiating FISH+ connection to %s:%s (user: %s)", host, port, user)
	client, err := DialSSH(host, port, user, pass, timeout)
	if err != nil {
		return nil, err
	}
	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	sess.Stderr = io.Discard
	if err := sess.Start("exec /bin/sh"); err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}

	title := host
	if user != "" {
		title = user + "@" + host
	}
	ctx, cancel := context.WithTimeout(context.Background(), sshTimeout(timeout))
	defer cancel()
	v, err := NewFishVFSOnStream(ctx, parent, stdin, stdout, &sshShell{sess: sess, client: client}, title)
	if err != nil {
		sess.Close()
		client.Close()
		return nil, err
	}
	vtui.DebugLog("NET: FISH+ session established, features: %s", v.client.Session().Features().Raw)
	return v, nil
}

// Client exposes the underlying protocol client, mostly so a caller can ask
// what the remote host turned out to be capable of.
func (v *FishVFS) Client() *fishplus.Client { return v.client }

func (v *FishVFS) GetTitle() string { return v.title }

func (v *FishVFS) IsAtRoot() bool      { return v.path == "/" || v.path == "" }
func (v *FishVFS) GetPath() string     { return v.path }
func (v *FishVFS) IsAbs(p string) bool { return path.IsAbs(p) }

func (v *FishVFS) Join(e ...string) string { return path.Join(e...) }
func (v *FishVFS) Base(p string) string    { return path.Base(p) }
func (v *FishVFS) Dir(p string) string     { return path.Dir(p) }

func (v *FishVFS) Abs(p string) (string, error) { return v.abs(p), nil }

func (v *FishVFS) abs(p string) string {
	if p == "" {
		return v.path
	}
	if path.IsAbs(p) {
		return path.Clean(p)
	}
	return path.Join(v.path, p)
}

func (v *FishVFS) SetPath(p string) error {
	target := v.abs(p)
	item, err := v.Stat(context.Background(), target)
	if err != nil {
		return err
	}
	if !item.IsDir {
		return os.ErrInvalid
	}
	v.path = target
	return nil
}

// entryToItem converts one remote entry. A symlink keeps its own mode bits,
// but the panel needs to know whether entering it lands in a directory: the
// find backend says so for free, the stat backends cost one extra round
// trip per link.
func (v *FishVFS) entryToItem(ctx context.Context, dir string, e fishplus.Entry) vfs.VFSItem {
	isDir := e.IsDir()
	if e.IsSymlink() {
		if e.TargetIsDir {
			isDir = true
		} else if target, err := v.client.Stat(ctx, path.Join(dir, e.Name)); err == nil {
			isDir = target.IsDir()
		}
	}
	return vfs.VFSItem{
		Name:         e.Name,
		Size:         e.Size,
		IsDir:        isDir,
		MTime:        e.MTime,
		ATime:        e.ATime,
		IsExecutable: e.IsExecutable(),
		IsHidden:     strings.HasPrefix(e.Name, "."),
		IsSymlink:    e.IsSymlink(),
		UnixMode:     e.Mode,
		Uid:          e.Uid,
		Gid:          e.Gid,
	}
}

func (v *FishVFS) ReadDir(ctx context.Context, p string, onChunk func([]vfs.VFSItem)) error {
	dir := v.abs(p)
	entries, err := v.client.Enum(ctx, dir)
	if err != nil {
		return err
	}
	items := make([]vfs.VFSItem, 0, fishReadDirChunk)
	for i, e := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		items = append(items, v.entryToItem(ctx, dir, e))
		if len(items) >= fishReadDirChunk || i == len(entries)-1 {
			onChunk(items)
			items = make([]vfs.VFSItem, 0, fishReadDirChunk)
		}
	}
	return nil
}

// Stat reports the link itself rather than its target, so the panel can draw
// a symlink as one, and only resolves it to answer the IsDir question.
func (v *FishVFS) Stat(ctx context.Context, p string) (vfs.VFSItem, error) {
	target := v.abs(p)
	e, err := v.client.Lstat(ctx, target)
	if err != nil {
		return vfs.VFSItem{}, err
	}
	return v.entryToItem(ctx, path.Dir(target), e), nil
}

func (v *FishVFS) Open(ctx context.Context, p string) (vfs.ReadAtCloser, error) {
	return v.client.Open(ctx, v.abs(p))
}

func (v *FishVFS) GetCapabilities() vfs.VFSCapabilities {
	return vfs.VFSCapabilities{
		HasRandomAccess:    true,
		HasUnixPermissions: true,
		HasSearch:          v.client.CanGrep(),
	}
}

// fishSearchMax caps how many matches one search brings back. It is there so
// that a pattern matching half a log cannot fill the panel's memory with
// offsets nobody will ever scroll to.
const fishSearchMax = 10000

// Search hands the pattern to the remote host's own grep and returns the byte
// offset of every match. Only the offsets cross the network, which is what
// lets a panel search a log it would take an hour to download. A host without
// the tools answers nil, and the caller falls back to reading the file,
// exactly as it does with SFTP.
func (v *FishVFS) Search(ctx context.Context, p, pattern string) (chan int64, error) {
	if pattern == "" || !v.client.CanGrep() {
		return nil, nil
	}
	offsets, err := v.client.Grep(ctx, v.abs(p), pattern, fishplus.GrepOptions{Fixed: true, Limit: fishSearchMax})
	if err != nil {
		return nil, err
	}
	// The channel is filled before it is handed over. The session answers one
	// request at a time anyway, so a producing goroutine would buy no overlap
	// and would only add a way for a caller that stops reading to leave it
	// hanging on a send.
	out := make(chan int64, len(offsets))
	for _, off := range offsets {
		out <- off
	}
	close(out)
	return out, nil
}

// FindFiles implements vfs.FileFinder. The remote host walks the tree and,
// when a content pattern is given, greps the candidates in the same pass, so
// what crosses the network is one request and one line per hit.
//
// A symlink is reported as found without resolving it: the alternative is a
// round trip per hit, which would give back what the whole command saves.
func (v *FishVFS) FindFiles(ctx context.Context, dir string, q vfs.FindQuery) ([]vfs.FoundEntry, error) {
	entries, err := v.client.Find(ctx, v.abs(dir), fishplus.FindOptions{
		Masks:      q.Masks,
		Text:       q.Text,
		Fixed:      true,
		IgnoreCase: q.IgnoreCase,
		Limit:      q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]vfs.FoundEntry, 0, len(entries))
	for _, e := range entries {
		name := path.Base(e.Name)
		out = append(out, vfs.FoundEntry{
			Path: e.Name,
			Item: vfs.VFSItem{
				Name:         name,
				Size:         e.Size,
				IsDir:        e.IsDir(),
				MTime:        e.MTime,
				ATime:        e.ATime,
				IsExecutable: e.IsExecutable(),
				IsHidden:     strings.HasPrefix(name, "."),
				IsSymlink:    e.IsSymlink(),
				UnixMode:     e.Mode,
				Uid:          e.Uid,
				Gid:          e.Gid,
			},
		})
	}
	return out, nil
}

// PatchFile implements vfs.DeltaWriter. The copying happens on the remote
// host at local disk speed; only the new bytes cross the network.
func (v *FishVFS) PatchFile(ctx context.Context, src, dst string, pieces []vfs.PatchPiece) error {
	if !v.client.CanPatch() {
		return fishplus.ErrNoWrite
	}
	segs := make([]fishplus.PatchSegment, 0, len(pieces))
	for _, p := range pieces {
		if p.Data == nil {
			segs = append(segs, fishplus.Copy(p.Offset, p.Length))
			continue
		}
		segs = append(segs, fishplus.Literal(p.Data))
	}
	return v.client.Patch(ctx, v.abs(src), v.abs(dst), segs)
}

// LineIndex implements vfs.LineIndexer. A count of zero asks for nothing but
// the total, which is one remote pass and three numbers on the wire.
func (v *FishVFS) LineIndex(ctx context.Context, p string, first, count int64) (vfs.LineIndexResult, error) {
	idx, err := v.client.Lines(ctx, v.abs(p), first, count)
	if err != nil {
		return vfs.LineIndexResult{}, err
	}
	return vfs.LineIndexResult{First: idx.First, Offsets: idx.Offsets, Total: idx.Total}, nil
}
func (v *FishVFS) MkDir(ctx context.Context, p string) error {
	return v.client.MkDir(ctx, v.abs(p))
}

// Remove deletes whatever is at the path. A directory is removed with
// everything below it by the remote host itself, in one round trip instead
// of one per entry, which is the main reason a shell based file system is
// worth having at all.
func (v *FishVFS) Remove(ctx context.Context, p string) error {
	target := v.abs(p)
	e, err := v.client.Lstat(ctx, target)
	if err != nil {
		return err
	}
	if e.IsDir() {
		return v.client.RemoveAll(ctx, target)
	}
	return v.client.Remove(ctx, target)
}

func (v *FishVFS) Rename(ctx context.Context, o, n string) error {
	return v.client.Rename(ctx, v.abs(o), v.abs(n))
}

// Create truncates the file, or creates it, and hands back a handle that
// streams from the beginning. The handle buffers up to one transfer chunk,
// so the copier's small writes do not each become a round trip.
func (v *FishVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	w, err := v.client.Create(ctx, v.abs(p))
	if err != nil {
		return nil, err
	}
	return w, nil
}

// SetAttributes applies the permission bits, then the ownership, then the
// timestamps. A Uid or Gid below zero means "leave that half alone", which
// is what the copier passes; a zero timestamp is filled in from the other
// one, the same way the SFTP backend does it, so a file copied onto a FISH+
// panel keeps the times it had.
func (v *FishVFS) SetAttributes(ctx context.Context, p string, item vfs.VFSItem) error {
	target := v.abs(p)
	if item.UnixMode != 0 {
		if err := v.client.Chmod(ctx, target, item.UnixMode); err != nil {
			return err
		}
	}
	if err := v.client.Chown(ctx, target, item.Uid, item.Gid); err != nil {
		return err
	}
	mtime, atime := item.MTime, item.ATime
	if mtime.IsZero() {
		mtime = atime
	}
	if atime.IsZero() {
		atime = mtime
	}
	return v.client.Chtimes(ctx, target, mtime, atime)
}

func (v *FishVFS) ParentVFS() vfs.VFS { return v.parent }

// Clone returns a second view of the same session, with its own current
// directory. A second login would cost another authentication and another
// password prompt, and would buy nothing: the remote shell answers one
// command at a time either way.
func (v *FishVFS) Clone() vfs.VFS {
	if v.conn != nil {
		v.conn.retain()
	}
	return &FishVFS{
		parent: v.parent,
		client: v.client,
		conn:   v.conn,
		path:   v.path,
		title:  v.title,
	}
}

// Close releases this view. The session itself goes away with its last
// user, and closing the same view twice is harmless: a panel may well be
// closed by both its own teardown and the frame that owned it.
func (v *FishVFS) Close() error {
	var err error
	v.once.Do(func() {
		if v.conn != nil {
			err = v.conn.release()
		}
	})
	return err
}

// fishTypeMatches reports whether a site configuration asks for FISH+. The
// plus is part of the name because the protocol is more than the classic
// fish, but a configuration that spells it without one is accepted too.
func fishTypeMatches(t string) bool {
	return t == "fish+" || t == "fish"
}

// netFoxConfigAt reads the site configuration a connection entry stands for.
func netFoxConfigAt(ctx context.Context, parent vfs.VFS, pth string) (NetFoxConfig, bool) {
	w, ok := parent.(*netFoxVFSWrapper)
	if !ok {
		return NetFoxConfig{}, false
	}
	item, err := w.Stat(ctx, pth)
	if err != nil || item.IsDir {
		return NetFoxConfig{}, false
	}
	f, err := w.Open(ctx, pth)
	if err != nil {
		return NetFoxConfig{}, false
	}
	defer f.Close()
	var cfg NetFoxConfig
	if err := json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg); err != nil {
		return NetFoxConfig{}, false
	}
	return cfg, true
}

type fishProvider struct{}

func (p *fishProvider) Name() string  { return "NetFox-FISH+" }
func (p *fishProvider) Priority() int { return 100 }

func (p *fishProvider) CanOpen(ctx context.Context, parent vfs.VFS, pth string) bool {
	cfg, ok := netFoxConfigAt(ctx, parent, pth)
	return ok && fishTypeMatches(cfg.Type)
}

func (p *fishProvider) Open(ctx context.Context, parent vfs.VFS, pth string) (vfs.VFS, error) {
	cfg, ok := netFoxConfigAt(ctx, parent, pth)
	if !ok {
		return nil, os.ErrInvalid
	}
	port := cfg.Port
	if port == "" {
		port = "22"
	}
	timeout := 15
	if cfg.Timeout != "" {
		if t, err := strconv.Atoi(cfg.Timeout); err == nil && t > 0 {
			timeout = t
		}
	}
	return NewFishVFS(parent, cfg.Host, port, cfg.User, cfg.Pass, timeout)
}

type fishProtocolHandler struct{}

func (ph *fishProtocolHandler) Prefix() string      { return "fish+" }
func (ph *fishProtocolHandler) DefaultPort() string { return "22" }
func (ph *fishProtocolHandler) BuildExtraUI(cfg *NetFoxConfig, x, y, w, h int) (vtui.UIElement, func()) {
	return nil, func() {}
}

func init() {
	vfs.RegisterProvider(&fishProvider{})
	RegisterProtocol(&fishProtocolHandler{})
}
