package vfs

import (
	"context"
	"io"
	"os"
	"path"
	"time"

	"github.com/jlaffaye/ftp"
	"github.com/unxed/vtui"
)

type FTPVFS struct {
	parent VFS
	conn   *ftp.ServerConn
	host   string
	port   string
	user   string
	pass   string
	cwd    string
}

func NewFTPVFS(parent VFS, host, port, user, pass string) (*FTPVFS, error) {
	addr := host + ":" + port
	vtui.DebugLog("FTP: Dialing %s (timeout 15s)...", addr)

	// Используем Dial с явным указанием контекста для поддержки отмены
	c, err := ftp.Dial(addr,
		ftp.DialWithTimeout(15*time.Second),
		ftp.DialWithDisabledEPSV(true),
		ftp.DialWithContext(context.Background()),
	)
	if err != nil {
		vtui.DebugLog("FTP: Dial failed: %v", err)
		return nil, err
	}

	vtui.DebugLog("FTP: Connected. Logging in as %q...", user)
	err = c.Login(user, pass)
	if err != nil {
		vtui.DebugLog("FTP: Login failed: %v", err)
		c.Quit()
		return nil, err
	}
	vtui.DebugLog("FTP: Login successful.")

	pwd, err := c.CurrentDir()
	if err != nil {
		pwd = "/"
	}

	return &FTPVFS{
		parent: parent,
		conn:   c,
		host:   host,
		port:   port,
		user:   user,
		pass:   pass,
		cwd:    pwd,
	}, nil
}
func (v *FTPVFS) IsAtRoot() bool { return v.cwd == "/" || v.cwd == "" }

func (v *FTPVFS) GetPath() string { return v.cwd }

func (v *FTPVFS) SetPath(p string) error {
	target := p
	if !path.IsAbs(p) {
		target = path.Join(v.cwd, p)
	}
	vtui.DebugLog("FTP: CWD to %q", target)
	err := v.conn.ChangeDir(target)
	if err != nil {
		vtui.DebugLog("FTP: ChangeDir failed: %v", err)
		return err
	}
	v.cwd = target
	return nil
}

func (v *FTPVFS) ReadDir(ctx context.Context, p string, onChunk func([]VFSItem)) error {
	// Для корня или текущей папки многие FTP серверы предпочитают пустую строку
	target := p
	if target == "/" || target == "." {
		target = ""
	}

	vtui.DebugLog("FTP: List %q...", target)
	entries, err := v.conn.List(target)
	if err != nil {
		vtui.DebugLog("FTP: List failed: %v", err)
		return err
	}
	vtui.DebugLog("FTP: List returned %d items", len(entries))

	var items []VFSItem
	for _, e := range entries {
		if e.Name == "." || e.Name == ".." {
			continue
		}
		typ := false
		if e.Type == ftp.EntryTypeFolder {
			typ = true
		}
		items = append(items, VFSItem{
			Name:  e.Name,
			Size:  int64(e.Size),
			IsDir: typ,
			MTime: e.Time,
		})
	}
	if len(items) > 0 {
		onChunk(items)
	}
	return nil
}

func (v *FTPVFS) Stat(ctx context.Context, p string) (VFSItem, error) {
	// FTP doesn't have a direct Stat for a path reliably, we list parent
	dir := path.Dir(p)
	base := path.Base(p)
	entries, err := v.conn.List(dir)
	if err != nil {
		return VFSItem{}, err
	}
	for _, e := range entries {
		if e.Name == base {
			return VFSItem{
				Name:  e.Name,
				Size:  int64(e.Size),
				IsDir: e.Type == ftp.EntryTypeFolder,
				MTime: e.Time,
			}, nil
		}
	}
	return VFSItem{}, os.ErrNotExist
}

func (v *FTPVFS) Join(e ...string) string   { return path.Join(e...) }
func (v *FTPVFS) Abs(p string) (string, error) { return path.Join(v.cwd, p), nil }
func (v *FTPVFS) Base(p string) string         { return path.Base(p) }
func (v *FTPVFS) Dir(p string) string          { return path.Dir(p) }

func (v *FTPVFS) MkDir(ctx context.Context, p string) error { return v.conn.MakeDir(p) }
func (v *FTPVFS) Remove(ctx context.Context, p string) error {
	// Try file delete first, then dir delete
	err := v.conn.Delete(p)
	if err != nil {
		return v.conn.RemoveDir(p)
	}
	return nil
}
func (v *FTPVFS) Rename(ctx context.Context, o, n string) error { return v.conn.Rename(o, n) }

func (v *FTPVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{HasRandomAccess: false} // FTP random access is tricky with REST
}

func (v *FTPVFS) Open(ctx context.Context, p string) (ReadAtCloser, error) {
	resp, err := v.conn.Retr(p)
	if err != nil {
		return nil, err
	}

	// Create temp file for ReadAtCloser support (similar to ArchiveVFS)
	tmp, err := os.CreateTemp("", "f4ftp-*")
	if err != nil {
		resp.Close()
		return nil, err
	}

	_, err = io.Copy(tmp, resp)
	resp.Close()
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, err
	}

	tmp.Seek(0, io.SeekStart)
	stat, _ := tmp.Stat()
	return &tempReadAtCloser{
		osFileWrapper: &osFileWrapper{File: tmp, size: stat.Size()},
		tempPath:      tmp.Name(),
	}, nil
}

func (v *FTPVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	pr, pw := io.Pipe()
	go func() {
		err := v.conn.Stor(p, pr)
		pr.CloseWithError(err)
	}()
	return pw, nil
}

func (v *FTPVFS) ParentVFS() VFS { return v.parent }
func (v *FTPVFS) Close() error   { return v.conn.Quit() }
func (v *FTPVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }