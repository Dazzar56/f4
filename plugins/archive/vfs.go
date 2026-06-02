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

	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
	"github.com/unxed/zip"
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

	format    archives.Format
	arcFS     fs.FS
	closer    io.Closer
	isZip     bool
	zipReader *zip.ReadCloser
	isTar     bool
	tarFS     any
}

func (v *ArchiveVFS) IsAtRoot() bool {
	return v.innerPath == "." || v.innerPath == ""
}

func NewArchiveVFS(parent vfs.VFS, path string) (*ArchiveVFS, error) {
	var arcFS fs.FS
	var err error
	var finalPath string
	var closer io.Closer

	if osvfs, ok := parent.(*vfs.OSVFS); ok {
		finalPath, _ = osvfs.Abs(path)
		arcFS, err = archives.FileSystem(context.Background(), finalPath, nil)
	} else {
		rc, openErr := parent.Open(context.Background(), path)
		if openErr != nil {
			return nil, openErr
		}

		// Use a local trick for nested archives: if it's a generic ReadAtCloser,
		// we might need to extract it to a temp file anyway.
		// For now, keeping the logic simplified as in original.
		tmp, _ := os.CreateTemp("", "f4nested-*")
		io.Copy(tmp, ctxReader{rc, context.Background()})
		rc.Close()
		finalPath = tmp.Name()
		closer = tmp
		arcFS, err = archives.FileSystem(context.Background(), finalPath, nil)
	}

	if err != nil {
		if closer != nil {
			closer.Close()
			os.Remove(finalPath)
		}
		return nil, err
	}

	f, _ := os.Open(finalPath)
	defer f.Close()
	format, _, _ := archives.Identify(context.Background(), finalPath, f)

	isZip := false
	var zr *zip.ReadCloser
	isTar := false
	var tfs any

	if _, ok := format.(archives.Zip); ok {
		if z, err := zip.OpenReader(finalPath); err == nil {
			isZip = true
			zr = z
			arcFS = z
		}
	} else if _, ok := format.(archives.Tar); ok {
		isTar = true
	} else if ca, ok := format.(archives.CompressedArchive); ok {
		if _, ok := ca.Archival.(archives.Tar); ok {
			isTar = true
		}
	}

	if isTar {
		if t, err := tarOpenFS(finalPath); err == nil {
			tfs = t
			arcFS = t.(fs.FS)
		} else {
			isTar = false
		}
	}

	return &ArchiveVFS{
		parent:    parent,
		arcPath:   path,
		innerPath: ".",
		format:    format,
		arcFS:     arcFS,
		closer:    closer,
		isZip:     isZip,
		zipReader: zr,
		isTar:     isTar,
		tarFS:     tfs,
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

	entries, err := fs.ReadDir(v.arcFS, fsPath)
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

	info, err := fs.Stat(v.arcFS, fsPath)
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

	srcFile, err := v.arcFS.Open(fsPath)
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	tmp, err := os.CreateTemp("", "f4arc-*")
	if err != nil {
		return nil, err
	}
	io.Copy(tmp, &ioCtxReader{r: srcFile, ctx: ctx}) // Use context-aware reader
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

	if v.isZip {
		tmp, _ := os.CreateTemp("", "f4arc-zip-write-*")
		return &archiveWriteWrapper{v: v, tmpFile: tmp, destPath: fsPath, isZip: true}, nil
	}

	inserter, ok := v.format.(archives.Inserter)
	if !ok {
		return nil, fmt.Errorf("format %v does not support modifications", v.format)
	}
	tmp, _ := os.CreateTemp("", "f4arc-write-*")
	return &archiveWriteWrapper{v: v, tmpFile: tmp, destPath: fsPath, inserter: inserter}, nil
}

type archiveWriteWrapper struct {
	v        *ArchiveVFS
	tmpFile  *os.File
	destPath string
	inserter archives.Inserter
	isZip    bool
}

func (w *archiveWriteWrapper) Write(p []byte) (n int, err error) { return w.tmpFile.Write(p) }
func (w *archiveWriteWrapper) Close() error {
	w.tmpFile.Close()
	tmpName := w.tmpFile.Name()
	defer os.Remove(tmpName)

	tempArcPath := w.v.arcPath + ".tmp"
	out, err := os.Create(tempArcPath)
	if err != nil {
		return err
	}
	defer func() { out.Close(); os.Remove(tempArcPath) }()

	in, err := os.Open(w.v.arcPath)
	if err != nil {
		return err
	}
	defer in.Close()

	if w.isZip {
		// ZIP-specific atomic update using unxed/zip.Updater
		// 1. Copy original archive to temp
		if _, err := io.Copy(out, in); err != nil {
			return err
		}
		// 2. Open Updater on the copy
		upd, err := zip.NewUpdater(out)
		if err != nil {
			return err
		}
		// 3. Append the new file
		zw, err := upd.Append(w.destPath, zip.APPEND_MODE_OVERWRITE)
		if err != nil {
			upd.Close()
			return err
		}
		w.tmpFile.Seek(0, io.SeekStart)
		if _, err := io.Copy(zw, w.tmpFile); err != nil {
			upd.Close()
			return err
		}
		// 4. Finalize Updater (writes directory)
		if err := upd.Close(); err != nil {
			return err
		}
	} else {
		// Other formats using archives.Inserter
		files := []archives.FileInfo{{
			NameInArchive: w.destPath,
			FileInfo:      dummyFileInfo{name: filepath.Base(w.destPath), tempName: tmpName},
			Open:          func() (fs.File, error) { return os.Open(tmpName) },
		}}
		err = w.inserter.Insert(context.Background(), out, files)
		if err != nil {
			return err
		}
	}

	in.Close()
	out.Close()

	err = os.Rename(tempArcPath, w.v.arcPath)
	if err == nil {
		w.v.reloadFS()
	}
	return err
}

type dummyFileInfo struct {
	name     string
	tempName string
}

func (d dummyFileInfo) Name() string       { return d.name }
func (d dummyFileInfo) Size() int64        { s, _ := os.Stat(d.tempName); return s.Size() }
func (d dummyFileInfo) Mode() fs.FileMode  { return 0644 }
func (d dummyFileInfo) ModTime() time.Time { return time.Now() }
func (d dummyFileInfo) IsDir() bool        { return false }
func (d dummyFileInfo) Sys() any           { return nil }

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
	inserter, ok := v.format.(archives.Inserter)
	if !ok {
		return fmt.Errorf("format %v does not support modifications", v.format)
	}
	archiveFile, _ := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	defer archiveFile.Close()
	files := []archives.FileInfo{{NameInArchive: fsPath, FileInfo: dummyDirInfo{name: filepath.Base(fsPath)}}}
	err := inserter.Insert(ctx, archiveFile, files)
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

	if v.isZip {
		f, err := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
		if err != nil {
			return err
		}
		defer f.Close()
		upd, err := zip.NewUpdater(f)
		if err != nil {
			return err
		}
		defer upd.Close()

		// Find index
		idx := -1
		for i, entry := range upd.Entries() {
			if entry.Name == fsPath || entry.Name == fsPath+"/" {
				idx = i
				break
			}
		}
		if idx == -1 {
			return os.ErrNotExist
		}
		_, err = upd.RemoveFile(idx)
		if err == nil {
			v.reloadFS()
		}
		return err
	}

	type archDeleter interface {
		Delete(context.Context, io.ReadWriteSeeker, []string) error
	}
	deleter, ok := v.format.(archDeleter)
	if !ok {
		return fmt.Errorf("format %v does not support deletion", v.format)
	}
	archiveFile, _ := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	defer archiveFile.Close()
	err := deleter.Delete(ctx, archiveFile, []string{fsPath})
	if err == nil {
		v.reloadFS()
	}
	return err
}

func (v *ArchiveVFS) reloadFS() {
	activePath := v.arcPath
	if f, ok := v.closer.(*os.File); ok {
		activePath = f.Name()
	}

	if v.isZip {
		if v.zipReader != nil {
			v.zipReader.Close()
		}
		if z, err := zip.OpenReader(activePath); err == nil {
			v.zipReader = z
			v.arcFS = z
		}
		return
	}
	if v.isTar {
		if v.tarFS != nil {
			v.tarFS.(io.Closer).Close()
			if v.closer != nil {
				tarRemoveIndex(activePath)
			}
		}
		if t, err := tarOpenFS(activePath); err == nil {
			v.tarFS = t
			v.arcFS = t.(fs.FS)
		}
		return
	}
	newFS, err := archives.FileSystem(context.Background(), activePath, nil)
	if err == nil {
		v.arcFS = newFS
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
	if v.zipReader != nil {
		v.zipReader.Close()
	}
	if v.isTar && v.tarFS != nil {
		v.tarFS.(io.Closer).Close()
		if v.closer != nil {
			tarRemoveIndex(v.arcPath)
		}
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
