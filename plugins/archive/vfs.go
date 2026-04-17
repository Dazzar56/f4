package archive

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sync"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/mholt/archives"
	"github.com/unxed/f4/vfs"
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
type ArchiveVFS struct {
	mu        sync.Mutex
	parent    vfs.VFS
	arcPath   string
	innerPath string

	format    archives.Format
	arcFS     fs.FS
	closer    io.Closer
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
		if openErr != nil { return nil, openErr }

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
		if closer != nil { closer.Close(); os.Remove(finalPath) }
		return nil, err
	}

	f, _ := os.Open(finalPath)
	defer f.Close()
	format, _, _ := archives.Identify(context.Background(), finalPath, f)

	return &ArchiveVFS{
		parent:    parent,
		arcPath:   path,
		innerPath: ".",
		format:    format,
		arcFS:     arcFS,
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

func (v *ArchiveVFS) SetPath(path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	newPath := filepath.ToSlash(filepath.Clean(path))
	prefix := filepath.ToSlash(filepath.Clean(v.arcPath))

	if strings.HasPrefix(newPath, prefix) {
		newPath = strings.TrimPrefix(newPath, prefix)
	}

	newPath = strings.TrimPrefix(newPath, "/")
	if newPath == "" { newPath = "." }
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
		if arcInfo, ok := info.(archives.FileInfo); ok {
			if hdr, ok := arcInfo.Header.(*zip.FileHeader); ok {
				name = DecodeZipName(hdr)
			}
		}

		items = append(items, vfs.VFSItem{
			Name:  name,
			IsDir: e.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime(),
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
	if err != nil { return vfs.VFSItem{}, err }

	return vfs.VFSItem{
		Name:  info.Name(),
		IsDir: info.IsDir(),
		Size:  info.Size(),
		MTime: info.ModTime(),
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
	if err != nil { return nil, err }
	defer srcFile.Close()

	tmp, err := os.CreateTemp("", "f4arc-*")
	if err != nil { return nil, err }
	io.Copy(tmp, srcFile) // arcFS.Open returns standard io.Reader, no context needed
	tmp.Seek(0, io.SeekStart)
	stat, _ := tmp.Stat()
	return &vfs.TempFileWrapper{File: tmp, SizeVal: stat.Size(), TempPath: tmp.Name()}, nil
}

func (v *ArchiveVFS) ParentVFS() vfs.VFS         { return v.parent }
func (v *ArchiveVFS) Join(e ...string) string { return filepath.ToSlash(filepath.Join(e...)) }
func (v *ArchiveVFS) Abs(p string) (string, error) { return v.Join(v.arcPath, p), nil }
func (v *ArchiveVFS) Base(p string) string    { return filepath.Base(p) }
func (v *ArchiveVFS) Dir(p string) string {
	if p == v.arcPath { return v.parent.Dir(v.arcPath) }
	return filepath.ToSlash(filepath.Dir(p))
}

func (v *ArchiveVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	inserter, ok := v.format.(archives.Inserter)
	if !ok { return nil, fmt.Errorf("format %v does not support modifications", v.format) }
	tmp, _ := os.CreateTemp("", "f4arc-write-*")

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	return &archiveWriteWrapper{v: v, tmpFile: tmp, destPath: fsPath, inserter: inserter}, nil
}

type archiveWriteWrapper struct {
	v        *ArchiveVFS
	tmpFile  *os.File
	destPath string
	inserter archives.Inserter
}

func (w *archiveWriteWrapper) Write(p []byte) (n int, err error) { return w.tmpFile.Write(p) }
func (w *archiveWriteWrapper) Close() error {
	w.tmpFile.Close()
	tmpName := w.tmpFile.Name()
	defer os.Remove(tmpName)

	// ATOMIC UPDATE:
	// 1. Create a temporary archive file
	tempArcPath := w.v.arcPath + ".tmp"
	out, err := os.Create(tempArcPath)
	if err != nil { return err }
	defer func() { out.Close(); os.Remove(tempArcPath) }()

	// 2. Open original archive for reading
	in, err := os.Open(w.v.arcPath)
	if err != nil { return err }
	defer in.Close()

	// 3. Prepare the new file entry
	files := []archives.FileInfo{{
		NameInArchive: w.destPath,
		FileInfo:      dummyFileInfo{name: filepath.Base(w.destPath), tempName: tmpName},
		Open:          func() (fs.File, error) { return os.Open(tmpName) },
	}}

	// 4. Perform insertion into the NEW (temp) archive file.
	// This ensures that if the process fails, the original archive is untouched.
	err = w.inserter.Insert(context.Background(), out, files)
	if err != nil { return err }

	// 5. Finalize: Close files and swap
	in.Close()
	out.Close()

	err = os.Rename(tempArcPath, w.v.arcPath)
	if err == nil { w.v.reloadFS() }
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

	if !strings.HasSuffix(fsPath, "/") { fsPath += "/" }
	inserter, ok := v.format.(archives.Inserter)
	if !ok { return fmt.Errorf("format %v does not support modifications", v.format) }
	archiveFile, _ := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	defer archiveFile.Close()
	files := []archives.FileInfo{{NameInArchive: fsPath, FileInfo: dummyDirInfo{name: filepath.Base(fsPath)}}}
	err := inserter.Insert(ctx, archiveFile, files)
	if err == nil { v.reloadFS() }
	return err
}

func (v *ArchiveVFS) Remove(ctx context.Context, path string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	normPath := filepath.ToSlash(path)
	normArcPath := filepath.ToSlash(v.arcPath)
	fsPath := strings.TrimPrefix(normPath, normArcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	type archDeleter interface { Delete(context.Context, io.ReadWriteSeeker, []string) error }
	deleter, ok := v.format.(archDeleter)
	if !ok { return fmt.Errorf("format %v does not support deletion", v.format) }
	archiveFile, _ := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	defer archiveFile.Close()
	err := deleter.Delete(ctx, archiveFile, []string{fsPath})
	if err == nil { v.reloadFS() }
	return err
}

func (v *ArchiveVFS) reloadFS() {
	newFS, err := archives.FileSystem(context.Background(), v.arcPath, nil)
	if err == nil { v.arcFS = newFS }
}

func (v *ArchiveVFS) Rename(ctx context.Context, o, n string) error { return fmt.Errorf("read-only") }
func (v *ArchiveVFS) GetCapabilities() vfs.VFSCapabilities { return vfs.VFSCapabilities{HasRandomAccess: true} }
func (v *ArchiveVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }
func (v *ArchiveVFS) Close() error {
	if v.closer != nil {
		err := v.closer.Close()
		if f, ok := v.closer.(*os.File); ok { os.Remove(f.Name()) }
		return err
	}
	return nil
}

func (v *ArchiveVFS) Clone() vfs.VFS {
	// Archive VFS is currently stateful and linked to temp files.
	// For now, return self as cloning requires extracting everything again.
	return v
}
