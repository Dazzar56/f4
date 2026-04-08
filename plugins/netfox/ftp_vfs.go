package netfox

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/unxed/f4/vfs"
)

import "sync"

type FTPVFS struct {
	mu     sync.Mutex
	parent vfs.VFS
	conn   *ftp.ServerConn
	cwd    string
}

func NewFTPVFS(parent vfs.VFS, host, port, user, pass string) (*FTPVFS, error) {
	addr := host + ":" + port
	c, err := ftp.Dial(addr, ftp.DialWithTimeout(15*time.Second))
	if err != nil { return nil, err }

	err = c.Login(user, pass)
	if err != nil { c.Quit(); return nil, err }

	pwd, err := c.CurrentDir()
	if err != nil { pwd = "/" }

	return &FTPVFS{parent: parent, conn: c, cwd: pwd}, nil
}

func (v *FTPVFS) IsAtRoot() bool { return v.cwd == "/" || v.cwd == "" }
func (v *FTPVFS) GetPath() string { return v.cwd }
func (v *FTPVFS) SetPath(p string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	target := p
	if !path.IsAbs(p) { target = path.Join(v.cwd, p) }
	if err := v.conn.ChangeDir(target); err != nil { return err }
	v.cwd = target
	return nil
}

func (v *FTPVFS) ReadDir(ctx context.Context, p string, onChunk func([]vfs.VFSItem)) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	target := p
	if target == "/" || target == "." { target = "" }
	entries, err := v.conn.List(target)
	if err != nil { return err }
	var items []vfs.VFSItem
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." { continue }
		items = append(items, vfs.VFSItem{
			Name: e.Name, Size: int64(e.Size),
			IsDir: e.Type == ftp.EntryTypeFolder, MTime: e.Time,
		})
	}
	onChunk(items)
	return nil
}

func (v *FTPVFS) Stat(ctx context.Context, p string) (vfs.VFSItem, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	dir, base := path.Dir(p), path.Base(p)
	entries, err := v.conn.List(dir)
	if err != nil { return vfs.VFSItem{}, err }
	for _, e := range entries {
		if e.Name == base {
			return vfs.VFSItem{
				Name: e.Name, Size: int64(e.Size),
				IsDir: e.Type == ftp.EntryTypeFolder, MTime: e.Time,
			}, nil
		}
	}
	return vfs.VFSItem{}, os.ErrNotExist
}

func (v *FTPVFS) Join(e ...string) string      { return path.Join(e...) }
func (v *FTPVFS) Abs(p string) (string, error) { return path.Join(v.cwd, p), nil }
func (v *FTPVFS) Base(p string) string         { return path.Base(p) }
func (v *FTPVFS) Dir(p string) string          { return path.Dir(p) }
func (v *FTPVFS) MkDir(ctx context.Context, p string) error { return v.conn.MakeDir(p) }
func (v *FTPVFS) Remove(ctx context.Context, p string) error {
	if err := v.conn.Delete(p); err != nil { return v.conn.RemoveDir(p) }
	return nil
}
func (v *FTPVFS) Rename(ctx context.Context, o, n string) error { return v.conn.Rename(o, n) }
func (v *FTPVFS) GetCapabilities() vfs.VFSCapabilities { return vfs.VFSCapabilities{} }
func (v *FTPVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

func (v *FTPVFS) Open(ctx context.Context, p string) (vfs.ReadAtCloser, error) {
	resp, err := v.conn.Retr(p)
	if err != nil { return nil, err }
	tmp, _ := os.CreateTemp("", "f4ftp-*")
	io.Copy(tmp, resp)
	resp.Close()
	tmp.Seek(0, 0)
	stat, _ := tmp.Stat()
	return &ftpFileWrapper{File: tmp, size: stat.Size(), path: tmp.Name()}, nil
}

func (v *FTPVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		err := v.conn.Stor(p, pr)
		pr.CloseWithError(err)
	}()
	return pw, nil
}

func (v *FTPVFS) ParentVFS() vfs.VFS { return v.parent }
func (v *FTPVFS) Close() error      { return v.conn.Quit() }
func (v *FTPVFS) Clone() vfs.VFS {
	return v
}
type ftpProvider struct{}
func (p *ftpProvider) Name() string  { return "NetFox-FTP" }
func (p *ftpProvider) Priority() int { return 100 }
func (p *ftpProvider) CanOpen(ctx context.Context, parent vfs.VFS, pth string) bool {
	w, ok := parent.(*netFoxVFSWrapper)
	if !ok { return false }
	item, err := w.Stat(ctx, pth)
	if err != nil || item.IsDir { return false }
	f, err := w.Open(ctx, pth)
	if err != nil { return false }
	defer f.Close()
	var cfg NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	return cfg.Type == "ftp"
}
func (p *ftpProvider) Open(ctx context.Context, parent vfs.VFS, pth string) (vfs.VFS, error) {
	w := parent.(*netFoxVFSWrapper)
	f, _ := w.Open(ctx, pth)
	defer f.Close()
	var cfg NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	port := cfg.Port
	if port == "" { port = "21" }
	return NewFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}

func init() {
	vfs.RegisterProvider(&ftpProvider{})
}

type ftpFileWrapper struct {
	*os.File
	size int64
	path string
}
func (w *ftpFileWrapper) Size() int64 { return w.size }
func (w *ftpFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) { return w.File.ReadAt(p, off) }
func (w *ftpFileWrapper) Read(ctx context.Context, p []byte) (int, error) { return w.File.Read(p) }
func (w *ftpFileWrapper) Close() error { w.File.Close(); return os.Remove(w.path) }