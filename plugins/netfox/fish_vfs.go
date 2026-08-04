package netfox

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/unxed/f4/plugins/netfox/fishplus"
	"github.com/unxed/f4/vfs"
)

// ErrFishReadOnly is what every mutation answers until step 5 of the FISH+
// plan lands. It is deliberately not os.ErrPermission: the remote host is
// not refusing anything, this build simply cannot write yet.
var ErrFishReadOnly = errors.New("fish: this build of FISH+ is read only")

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
	return vfs.VFSCapabilities{HasRandomAccess: true, HasUnixPermissions: true}
}

// Search will be answered by the remote host itself in step 7; until then
// the caller falls back to reading the file, exactly as with SFTP.
func (v *FishVFS) Search(ctx context.Context, p, pattern string) (chan int64, error) {
	return nil, nil
}

func (v *FishVFS) MkDir(ctx context.Context, p string) error  { return ErrFishReadOnly }
func (v *FishVFS) Remove(ctx context.Context, p string) error { return ErrFishReadOnly }
func (v *FishVFS) Rename(ctx context.Context, o, n string) error {
	return ErrFishReadOnly
}
func (v *FishVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	return nil, ErrFishReadOnly
}
func (v *FishVFS) SetAttributes(ctx context.Context, p string, item vfs.VFSItem) error {
	return ErrFishReadOnly
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
