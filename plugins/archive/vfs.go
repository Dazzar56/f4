package archive

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/zipper/archive"
)

type dummyDirInfo struct {
	name string
}

func (d dummyDirInfo) Name() string       { return d.name }
func (d dummyDirInfo) Size() int64        { return 0 }
func (d dummyDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (d dummyDirInfo) ModTime() time.Time { return time.Now() }
func (d dummyDirInfo) IsDir() bool        { return true }
func (d dummyDirInfo) Sys() any           { return nil }

type ctxReader struct {
	r   vfs.ReadAtCloser
	ctx context.Context
}

func (cr ctxReader) Read(p []byte) (int, error) {
	return cr.r.Read(cr.ctx, p)
}

type nopWriteCloser struct {
	io.Writer
}

func (n *nopWriteCloser) Close() error { return nil }

type ArchiveVFS struct {
	mu        sync.Mutex
	parent    vfs.VFS
	arcPath   string
	innerPath string

	fsys      archive.FileSystem
	closer    io.Closer
}

func (v *ArchiveVFS) IsAtRoot() bool {
	return v.innerPath == "." || v.innerPath == ""
}

func (v *ArchiveVFS) activePath() string {
	if f, ok := v.closer.(*os.File); ok {
		return f.Name()
	}
	if osvfs, ok := v.parent.(*vfs.OSVFS); ok {
		absPath, _ := osvfs.Abs(v.arcPath)
		return absPath
	}
	return v.arcPath
}

func NewArchiveVFS(parent vfs.VFS, path string) (*ArchiveVFS, error) {
	var err error
	var finalPath string
	var closer io.Closer

	if osvfs, ok := parent.(*vfs.OSVFS); ok {
		finalPath, _ = osvfs.Abs(path)
	} else {
		rc, openErr := parent.Open(context.Background(), path)
		if openErr != nil {
			return nil, openErr
		}

		tmp, _ := os.CreateTemp("", "f4nested-*")
		io.Copy(tmp, ctxReader{rc, context.Background()})
		rc.Close()
		finalPath = tmp.Name()
		closer = tmp
	}

	fsys, err := archive.OpenFS(finalPath, archive.Options{})
	if err != nil {
		if closer != nil {
			closer.Close()
			os.Remove(finalPath)
		}
		return nil, err
	}

	return &ArchiveVFS{
		parent:    parent,
		arcPath:   path,
		innerPath: ".",
		fsys:      fsys,
		closer:    closer,
	}, nil
}

func (v *ArchiveVFS) GetPath() string {
	if v.innerPath == "." || v.innerPath == "" {
		return filepath.Clean(v.arcPath)
	}
	// Мы возвращаем нативный путь ОС, объединяя путь к архиву и внутренний путь
	return filepath.Join(v.arcPath, filepath.FromSlash(v.innerPath))
}
func (v *ArchiveVFS) IsAbs(p string) bool { return path.IsAbs(p) || strings.HasPrefix(p, v.arcPath) }

func (v *ArchiveVFS) SetPath(path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	newPath := filepath.ToSlash(filepath.Clean(path))
	prefix := filepath.ToSlash(filepath.Clean(v.arcPath))

	if strings.HasPrefix(newPath, prefix) {
		newPath = strings.TrimPrefix(newPath, prefix)
	}

	newPath = strings.TrimPrefix(newPath, "/")
	if newPath == "" {
		newPath = "."
	}
	v.innerPath = newPath
	return nil
}

func (v *ArchiveVFS) ReadDir(ctx context.Context, path string, onChunk func([]vfs.VFSItem)) error {
	v.mu.Lock()
	fsPath := v.innerPath
	if path != "" && path != v.GetPath() {
		if path == v.arcPath || path == v.arcPath+"/" || path == v.arcPath+"\\" {
			fsPath = "."
		} else {
			fsPath = strings.TrimPrefix(path, v.arcPath)
			fsPath = strings.TrimPrefix(fsPath, "/")
			fsPath = strings.TrimPrefix(fsPath, "\\")
		}
	}

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath = "."
	if normPath != normArcPath {
		fsPath = strings.TrimPrefix(normPath, normArcPath)
		fsPath = strings.TrimPrefix(fsPath, "/")
	}

	entries, err := fs.ReadDir(v.fsys, fsPath)
	if err != nil {
		v.mu.Unlock()
		return err
	}

	items := make([]vfs.VFSItem, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		name := e.Name()

		items = append(items, vfs.VFSItem{
			Name:     name,
			IsDir:    e.IsDir(),
			Size:     info.Size(),
			MTime:    info.ModTime(),
			IsHidden: strings.HasPrefix(name, "."),
		})
	}
	v.mu.Unlock()
	onChunk(items)
	return nil
}

func (v *ArchiveVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := "."
	if normPath != normArcPath {
		fsPath = strings.TrimPrefix(normPath, normArcPath)
		fsPath = strings.TrimPrefix(fsPath, "/")
	}

	info, err := fs.Stat(v.fsys, fsPath)
	if err != nil {
		return vfs.VFSItem{}, err
	}

	return vfs.VFSItem{
		Name:     info.Name(),
		IsDir:    info.IsDir(),
		Size:     info.Size(),
		MTime:    info.ModTime(),
		IsHidden: strings.HasPrefix(info.Name(), "."),
	}, nil
}

func (v *ArchiveVFS) Open(ctx context.Context, path string) (vfs.ReadAtCloser, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := "."
	if normPath != normArcPath {
		fsPath = strings.TrimPrefix(normPath, normArcPath)
		fsPath = strings.TrimPrefix(fsPath, "/")
	}

	srcFile, err := v.fsys.Open(fsPath)
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	tmp, err := os.CreateTemp("", "f4arc-*")
	if err != nil {
		return nil, err
	}
	io.Copy(tmp, srcFile)
	tmp.Seek(0, io.SeekStart)
	stat, _ := tmp.Stat()
	return &vfs.TempFileWrapper{File: tmp, SizeVal: stat.Size(), TempPath: tmp.Name()}, nil
}

func (v *ArchiveVFS) ParentVFS() vfs.VFS      { return v.parent }
func (v *ArchiveVFS) Join(e ...string) string { return filepath.ToSlash(filepath.Join(e...)) }
func (v *ArchiveVFS) Abs(p string) (string, error) {
	if v.IsAbs(p) {
		return filepath.ToSlash(filepath.Clean(p)), nil
	}
	return v.Join(v.GetPath(), p), nil
}
func (v *ArchiveVFS) Base(p string) string { return filepath.Base(p) }
func (v *ArchiveVFS) Dir(p string) string {
	if p == v.arcPath {
		return v.parent.Dir(v.arcPath)
	}
	return filepath.ToSlash(filepath.Dir(p))
}

func (v *ArchiveVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	tmp, _ := os.CreateTemp("", "f4arc-write-*")
	return &archiveWriteWrapper{v: v, tmpFile: tmp, destPath: fsPath}, nil
}

type archiveWriteWrapper struct {
	v        *ArchiveVFS
	tmpFile  *os.File
	destPath string
}

func (w *archiveWriteWrapper) Write(p []byte) (n int, err error) { return w.tmpFile.Write(p) }
func (w *archiveWriteWrapper) Close() error {
	w.tmpFile.Close()
	tmpName := w.tmpFile.Name()
	defer os.Remove(tmpName)

	upd, err := archive.NewUpdater(w.v.activePath(), archive.Options{})
	if err != nil {
		return err
	}
	defer upd.Close()

	w.tmpFile, err = os.Open(tmpName)
	if err != nil {
		return err
	}
	defer w.tmpFile.Close()

	stat, _ := w.tmpFile.Stat()
	err = upd.Append(w.destPath, stat.Size(), w.tmpFile)
	if err == nil {
		w.v.reloadFS()
	}
	return err
}

func (v *ArchiveVFS) MkDir(ctx context.Context, path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	if !strings.HasSuffix(fsPath, "/") {
		fsPath += "/"
	}

	upd, err := archive.NewUpdater(v.activePath(), archive.Options{})
	if err != nil {
		return err
	}
	defer upd.Close()

	err = upd.Append(fsPath, 0, nil)
	if err == nil {
		v.reloadFS()
	}
	return err
}

func (v *ArchiveVFS) Remove(ctx context.Context, path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	upd, err := archive.NewUpdater(v.activePath(), archive.Options{})
	if err != nil {
		return err
	}
	defer upd.Close()

	err = upd.Remove(fsPath)
	if err == nil {
		v.reloadFS()
	}
	return err
}

func (v *ArchiveVFS) reloadFS() {
	if v.fsys != nil {
		v.fsys.Close()
	}
	newFS, err := archive.OpenFS(v.activePath(), archive.Options{})
	if err == nil {
		v.fsys = newFS
	}
}

func (v *ArchiveVFS) Rename(ctx context.Context, o, n string) error { return fmt.Errorf("read-only") }

func (v *ArchiveVFS) SetAttributes(ctx context.Context, path string, item vfs.VFSItem) error {
	return fmt.Errorf("SetAttributes not supported for Archives yet")
}

func (v *ArchiveVFS) GetCapabilities() vfs.VFSCapabilities {
	return vfs.VFSCapabilities{HasRandomAccess: true, HasUnixPermissions: runtime.GOOS != "windows"}
}
func (v *ArchiveVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }
func (v *ArchiveVFS) Close() error {
	if v.fsys != nil {
		v.fsys.Close()
	}
	if v.closer != nil {
		err := v.closer.Close()
		if f, ok := v.closer.(*os.File); ok {
			os.Remove(f.Name())
		}
		return err
	}
	return nil
}

func (v *ArchiveVFS) Clone() vfs.VFS {
	// Archive VFS is currently stateful and linked to temp files.
	// For now, return self as cloning requires extracting everything again.
	return v
}
