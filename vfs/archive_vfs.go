package vfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/klauspost/compress/zip"
	"github.com/mholt/archives"
)
// dummyDirInfo реализует fs.FileInfo для виртуальных папок
type dummyDirInfo struct {
	name string
}

func (d dummyDirInfo) Name() string       { return d.name }
func (d dummyDirInfo) Size() int64        { return 0 }
func (d dummyDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (d dummyDirInfo) ModTime() time.Time { return time.Now() }
func (d dummyDirInfo) IsDir() bool        { return true }
func (d dummyDirInfo) Sys() any           { return nil }

type ArchiveVFS struct {
	parent    VFS
	arcPath   string // Абсолютный путь к файлу архива в родительской VFS
	innerPath string // Путь внутри архива (относительно корня архива, "." для корня)

	format    archives.Format
	arcFS     fs.FS
}

func NewArchiveVFS(parent VFS, path string) (*ArchiveVFS, error) {
	absPath, _ := parent.Abs(path)
	
	// Используем стандартный контекст для открытия
	arcFS, err := archives.FileSystem(context.Background(), absPath, nil)
	if err != nil {
		return nil, err
	}

	// Нам нужно знать формат, чтобы понимать, применять ли эвристику ZIP
	f, _ := os.Open(absPath)
	defer f.Close()
	format, _, _ := archives.Identify(context.Background(), absPath, f)

	return &ArchiveVFS{
		parent:    parent,
		arcPath:   path,
		innerPath: ".",
		format:    format,
		arcFS:     arcFS,
	}, nil
}

func (v *ArchiveVFS) IsAtRoot() bool {
	return v.innerPath == "." || v.innerPath == ""
}

func (v *ArchiveVFS) GetPath() string {
	if v.innerPath == "." || v.innerPath == "" {
		return v.arcPath
	}
	// Убираем возможные двойные слеши и лишние точки
	return filepath.ToSlash(filepath.Join(v.arcPath, v.innerPath))
}

func (v *ArchiveVFS) SetPath(path string) error {
	// Если нам передали полный путь (начинающийся с v.arcPath), отрезаем префикс.
	// Это фиксит баг со скриншота.
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

func (v *ArchiveVFS) ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error {
	fsPath := v.innerPath
	// Если запрошен путь, отличный от текущего
	if path != "" && path != v.GetPath() {
		fsPath = strings.TrimPrefix(path, v.arcPath + "/")
	}

	entries, err := fs.ReadDir(v.arcFS, fsPath)
	if err != nil { return err }

	items := make([]VFSItem, 0, len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		name := e.Name()

		// Применяем эвристику ZIP
		if arcInfo, ok := info.(archives.FileInfo); ok {
			if hdr, ok := arcInfo.Header.(*zip.FileHeader); ok {
				name = DecodeZipName(hdr)
			}
		}

		items = append(items, VFSItem{
			Name:  name,
			IsDir: e.IsDir(),
			Size:  info.Size(),
			MTime: info.ModTime(),
		})
	}
	onChunk(items)
	return nil
}

func (v *ArchiveVFS) Stat(ctx context.Context, path string) (VFSItem, error) {
	fsPath := strings.TrimPrefix(path, v.arcPath + "/")
	if fsPath == path { fsPath = "." }

	info, err := fs.Stat(v.arcFS, fsPath)
	if err != nil { return VFSItem{}, err }

	return VFSItem{
		Name:  info.Name(),
		IsDir: info.IsDir(),
		Size:  info.Size(),
		MTime: info.ModTime(),
	}, nil
}

func (v *ArchiveVFS) Open(ctx context.Context, path string) (ReadAtCloser, error) {
	fsPath := strings.TrimPrefix(path, v.arcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")
	if fsPath == "" { fsPath = "." }

	srcFile, err := v.arcFS.Open(fsPath)
	if err != nil { return nil, err }
	defer srcFile.Close()

	// Извлекаем во временный файл для Random Access ( ReadAt )
	tmp, err := os.CreateTemp("", "f4arc-*")
	if err != nil { return nil, err }
	
	if _, err := io.Copy(tmp, srcFile); err != nil {
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

func (v *ArchiveVFS) ParentVFS() VFS         { return v.parent }
func (v *ArchiveVFS) Join(e ...string) string { return filepath.ToSlash(filepath.Join(e...)) }
func (v *ArchiveVFS) Abs(p string) (string, error) { return v.Join(v.arcPath, p), nil }
func (v *ArchiveVFS) Base(p string) string    { return filepath.Base(p) }
func (v *ArchiveVFS) Dir(p string) string {
	if p == v.arcPath { return v.parent.Dir(v.arcPath) }
	return filepath.ToSlash(filepath.Dir(p))
}

func (v *ArchiveVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	inserter, ok := v.format.(archives.Inserter)
	if !ok {
		return nil, fmt.Errorf("format %v does not support modifications", v.format)
	}

	tmp, err := os.CreateTemp("", "f4arc-write-*")
	if err != nil {
		return nil, err
	}

	// Отрезаем путь архива, чтобы получить внутренний путь
	fsPath := strings.TrimPrefix(path, v.arcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	return &archiveWriteWrapper{
		v:        v,
		tmpFile:  tmp,
		destPath: fsPath,
		inserter: inserter,
	}, nil
}

type archiveWriteWrapper struct {
	v        *ArchiveVFS
	tmpFile  *os.File
	destPath string
	inserter archives.Inserter
}

func (w *archiveWriteWrapper) Write(p []byte) (n int, err error) {
	return w.tmpFile.Write(p)
}

func (w *archiveWriteWrapper) Close() error {
	w.tmpFile.Close()
	tmpName := w.tmpFile.Name()
	defer os.Remove(tmpName)

	archiveFile, err := os.OpenFile(w.v.arcPath, os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer archiveFile.Close()

	// Нам нужно передать archives.FileInfo с методом Open,
	// который откроет наш временный файл для чтения библиотекой.
	files := []archives.FileInfo{
		{
			NameInArchive: w.destPath,
			FileInfo:      dummyFileInfo{name: filepath.Base(w.destPath), tempName: tmpName},
			Open: func() (fs.File, error) {
				return os.Open(tmpName)
			},
		},
	}

	err = w.inserter.Insert(context.Background(), archiveFile, files)
	if err == nil {
		w.v.reloadFS()
	}
	return err
}

// dummyFileInfo для обычных файлов (реализует fs.FileInfo)
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
	// Для архивов MkDir — это создание пустой записи с именем, кончающимся на /
	fsPath := strings.TrimPrefix(path, v.arcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")
	if !strings.HasSuffix(fsPath, "/") {
		fsPath += "/"
	}

	inserter, ok := v.format.(archives.Inserter)
	if !ok {
		return fmt.Errorf("format %v does not support modifications", v.format)
	}

	// Готовим "виртуальный" файл для вставки
	archiveFile, err := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	if err != nil { return err }
	defer archiveFile.Close()

	// Inserter ожидает []archives.FileInfo
	files := []archives.FileInfo{
		{
			NameInArchive: fsPath,
			FileInfo:      dummyDirInfo{name: filepath.Base(fsPath)},
		},
	}

	err = inserter.Insert(ctx, archiveFile, files)
	if err == nil {
		// Обновляем состояние чтения
		v.reloadFS()
	}
	return err
}

func (v *ArchiveVFS) Remove(ctx context.Context, path string) error {
	fsPath := strings.TrimPrefix(path, v.arcPath)
	fsPath = strings.TrimPrefix(fsPath, "/")

	// Используем анонимный интерфейс, так как в некоторых версиях библиотеки
	// Deleter может не быть экспортирован глобально
	type archDeleter interface {
		Delete(context.Context, io.ReadWriteSeeker, []string) error
	}

	deleter, ok := v.format.(archDeleter)
	if !ok {
		return fmt.Errorf("format %v does not support deletion", v.format)
	}

	archiveFile, err := os.OpenFile(v.arcPath, os.O_RDWR, 0644)
	if err != nil { return err }
	defer archiveFile.Close()

	err = deleter.Delete(ctx, archiveFile, []string{fsPath})
	if err == nil {
		v.reloadFS()
	}
	return err
}

// reloadFS переоткрывает файловую систему архива после модификации
func (v *ArchiveVFS) reloadFS() {
	newFS, err := archives.FileSystem(context.Background(), v.arcPath, nil)
	if err == nil {
		v.arcFS = newFS
	}
}

func (v *ArchiveVFS) Rename(ctx context.Context, o, n string) error { return fmt.Errorf("read-only") }
func (v *ArchiveVFS) GetCapabilities() VFSCapabilities { return VFSCapabilities{HasRandomAccess: true} }
func (v *ArchiveVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

func (v *ArchiveVFS) IsArchive(ctx context.Context, path string) bool {
	// Nested archives (archives inside archives) are not yet supported for performance reasons
	return false
}

func (v *ArchiveVFS) OpenArchive(ctx context.Context, path string) (VFS, error) {
	return nil, fmt.Errorf("nested archives not supported")
}

type tempReadAtCloser struct {
	*osFileWrapper
	tempPath string
}

func (t *tempReadAtCloser) Close() error {
	t.osFileWrapper.Close()
	return os.Remove(t.tempPath)
}
