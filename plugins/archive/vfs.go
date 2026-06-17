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

	fsys   archive.FileSystem
	closer io.Closer

	activeCount  int
	isClosed     bool
	cleanupTimer *time.Timer
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

		tmp, errTemp := os.CreateTemp("", "f4nested-*")
		if errTemp != nil {
			rc.Close()
			return nil, errTemp
		}
		if _, errCopy := io.Copy(tmp, ctxReader{rc, context.Background()}); errCopy != nil {
			rc.Close()
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, errCopy
		}
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
	if v.fsys == nil {
		v.mu.Unlock()
		return fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}
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
	if v.fsys == nil {
		return vfs.VFSItem{}, fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}

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

type archiveReadWrapper struct {
	v          *ArchiveVFS
	once       sync.Once
	mu         sync.Mutex
	f          fs.File
	size       int64
	tmpFile    *os.File
	tmpPath    string
	extracted  bool
	extracting bool
	doneChan   chan struct{}
	err        error
}

func (w *archiveReadWrapper) Size() int64 {
	return w.size
}

func (w *archiveReadWrapper) Close() error {
	var err error
	w.once.Do(func() {
		w.mu.Lock()
		if w.f != nil {
			w.f.Close()
			w.f = nil
		}
		if w.tmpFile != nil {
			w.tmpFile.Close()
			os.Remove(w.tmpPath)
			w.tmpFile = nil
		}
		w.mu.Unlock()
		w.v.decrementActive()
	})
	return err
}

func (w *archiveReadWrapper) TempPath() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.tmpPath
}

func (w *archiveReadWrapper) extractToTemp(ctx context.Context) {
	w.mu.Lock()
	f := w.f
	w.mu.Unlock()

	if f == nil {
		return
	}

	tmp, err := os.CreateTemp("", "f4arc-*")
	if err != nil {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		return
	}

	w.mu.Lock()
	w.tmpPath = tmp.Name()
	w.tmpFile = tmp
	w.mu.Unlock()

	buf := make([]byte, 128*1024)
	var loopErr error

	for {
		if ctx.Err() != nil {
			loopErr = ctx.Err()
			break
		}
		n, errRead := f.Read(buf)
		if n > 0 {
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				loopErr = werr
				break
			}
		}
		if errRead != nil {
			if errRead != io.EOF {
				loopErr = errRead
			}
			break
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.f != nil {
		w.f.Close()
		w.f = nil
	}

	if loopErr != nil {
		w.err = loopErr
	} else {
		w.extracted = true
	}
}

func (w *archiveReadWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	w.mu.Lock()
	for !w.extracted && w.err == nil {
		if w.extracting {
			ch := w.doneChan
			w.mu.Unlock()
			select {
			case <-ch:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
			w.mu.Lock()
			continue
		}

		w.extracting = true
		w.doneChan = make(chan struct{})
		w.mu.Unlock()

		w.extractToTemp(ctx)

		w.mu.Lock()
		w.extracting = false
		close(w.doneChan)
		w.doneChan = nil
	}

	if w.err != nil {
		w.mu.Unlock()
		return 0, w.err
	}
	tmp := w.tmpFile
	w.mu.Unlock()

	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return tmp.ReadAt(p, off)
}

func (w *archiveReadWrapper) Read(ctx context.Context, p []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	w.mu.Lock()
	if w.err != nil {
		w.mu.Unlock()
		return 0, w.err
	}
	if w.extracted {
		tmp := w.tmpFile
		w.mu.Unlock()
		return tmp.Read(p)
	}

	f := w.f
	w.mu.Unlock()

	return f.Read(p)
}

func (v *ArchiveVFS) Open(ctx context.Context, path string) (vfs.ReadAtCloser, error) {
	v.mu.Lock()
	if v.fsys == nil {
		v.mu.Unlock()
		return nil, fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := "."
	if normPath != normArcPath {
		fsPath = strings.TrimPrefix(normPath, normArcPath)
		fsPath = strings.TrimPrefix(fsPath, "/")
	}

	srcFile, err := v.fsys.Open(fsPath)
	if err != nil {
		v.mu.Unlock()
		return nil, err
	}

	info, err := srcFile.Stat()
	var size int64
	if err == nil {
		size = info.Size()
	}

	v.activeCount++
	v.mu.Unlock()

	return &archiveReadWrapper{
		v:    v,
		f:    srcFile,
		size: size,
	}, nil
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
	if v.fsys == nil {
		v.mu.Unlock()
		return nil, fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	tmp, _ := os.CreateTemp("", "f4arc-write-*")
	v.activeCount++
	v.mu.Unlock()

	return &archiveWriteWrapper{v: v, tmpFile: tmp, destPath: fsPath}, nil
}

type archiveWriteWrapper struct {
	v        *ArchiveVFS
	tmpFile  *os.File
	destPath string
	once     sync.Once
}

func (w *archiveWriteWrapper) Write(p []byte) (n int, err error) { return w.tmpFile.Write(p) }
func (w *archiveWriteWrapper) Close() error {
	var err error
	w.once.Do(func() {
		w.tmpFile.Close()
		tmpName := w.tmpFile.Name()
		defer os.Remove(tmpName)

		w.v.mu.Lock()
		isClosed := w.v.isClosed
		w.v.mu.Unlock()

		if !isClosed {
			upd, errUpd := archive.NewUpdater(w.v.activePath(), archive.Options{})
			if errUpd == nil {
				defer upd.Close()
				w.tmpFile, err = os.Open(tmpName)
				if err == nil {
					defer w.tmpFile.Close()
					stat, _ := w.tmpFile.Stat()
					err = upd.Append(w.destPath, stat.Size(), w.tmpFile)
					if err == nil {
						w.v.reloadFS()
					}
				}
			} else {
				err = errUpd
			}
		} else {
			err = fmt.Errorf("archive VFS was closed")
		}
		w.v.decrementActive()
	})
	return err
}

func (v *ArchiveVFS) MkDir(ctx context.Context, path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.fsys == nil {
		return fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}

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
	if v.fsys == nil {
		return fmt.Errorf("archive VFS is closed")
	}
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}

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
	v.mu.Lock()
	defer v.mu.Unlock()

	v.isClosed = true
	if v.activeCount > 0 {
		return nil
	}

	v.startCleanupTimer()
	return nil
}

func (v *ArchiveVFS) decrementActive() {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.activeCount--
	if v.activeCount == 0 && v.isClosed {
		v.startCleanupTimer()
	}
}

func (v *ArchiveVFS) startCleanupTimer() {
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
	}
	// 2-second grace period of complete inactivity
	v.cleanupTimer = time.AfterFunc(2*time.Second, func() {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.activeCount == 0 && v.isClosed {
			v.performCleanup()
		}
	})
}

func (v *ArchiveVFS) performCleanup() error {
	if v.cleanupTimer != nil {
		v.cleanupTimer.Stop()
		v.cleanupTimer = nil
	}
	if v.fsys != nil {
		v.fsys.Close()
		v.fsys = nil
	}
	if v.closer != nil {
		err := v.closer.Close()
		if f, ok := v.closer.(*os.File); ok {
			os.Remove(f.Name())
		}
		v.closer = nil
		return err
	}
	return nil
}

func (v *ArchiveVFS) Clone() vfs.VFS {
	// Archive VFS is currently stateful and linked to temp files.
	// For now, return self as cloning requires extracting everything again.
	return v
}
