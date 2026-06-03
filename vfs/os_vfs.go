package vfs

import (
	"context"
	"io"
	"os"
	"time"

	"path/filepath"
	"strings"

	"runtime"

	"github.com/unxed/vtui"
)

type OSVFS struct {
	currentPath string
}

func NewOSVFS(initialPath string) *OSVFS {
	abs, _ := filepath.Abs(initialPath)
	return &OSVFS{currentPath: abs}
}

func (v *OSVFS) GetPath() string        { return v.currentPath }
func (v *OSVFS) IsAbs(path string) bool { return filepath.IsAbs(path) }

func (v *OSVFS) IsAtRoot() bool {
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(v.currentPath)
		p := filepath.Clean(v.currentPath)
		// Standardize to backslash for comparison on Windows
		p = strings.ReplaceAll(p, "/", "\\")
		vol = strings.ReplaceAll(vol, "/", "\\")
		return p == vol || p == vol+"." || p == vol+"\\" || p == "\\"
	}
	return v.currentPath == "/"
}

func (v *OSVFS) SetPath(path string) error {
	vtui.DebugLog("VFS: SetPath(%q) called", path)
	target := path
	if !filepath.IsAbs(path) && filepath.VolumeName(path) == "" {
		target = filepath.Join(v.currentPath, path)
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	// Resolve symlinks/junctions to avoid ACL issues on the link itself (e.g. "Documents and Settings")
	if resolved, errEval := filepath.EvalSymlinks(abs); errEval == nil {
		if runtime.GOOS == "windows" {
			origVol := filepath.VolumeName(abs)
			resVol := filepath.VolumeName(resolved)
			// Prevent resolving mapped drives (e.g. T:\) into UNC paths (\\server\share)
			if len(origVol) == 2 && origVol[1] == ':' && len(resVol) > 2 && strings.HasPrefix(resVol, `\\`) {
				abs = origVol + strings.TrimPrefix(resolved, resVol)
			} else {
				abs = resolved
			}
		} else {
			abs = resolved
		}
	}

	st, err := os.Stat(abs)
	if err != nil {
		if os.IsPermission(err) && globalSudoClient.IsAvailable() {
			vtui.DebugLog("VFS: SetPath: Permission denied for %q, checking via sudo...", abs)
			item, sudoErr := globalSudoClient.Stat(abs)
			if sudoErr == nil {
				if item.IsDir {
					vtui.DebugLog("VFS: Path changed to %q (via sudo Stat)", abs)
					v.currentPath = abs
					return nil
				}
				vtui.DebugLog("VFS: SetPath(%q) FAILED: not a directory (via sudo Stat)", abs)
				return os.ErrInvalid
			}
			return sudoErr
		}
		return err
	}
	if !st.IsDir() {
		vtui.DebugLog("VFS: SetPath(%q) FAILED: not a directory", abs)
		return os.ErrInvalid
	}
	vtui.DebugLog("VFS: Path changed to %q", abs)
	v.currentPath = abs
	return nil
}

func (v *OSVFS) ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsPermission(err) && globalSudoClient.IsAvailable() {
			vtui.DebugLog("VFS: Permission denied for ReadDir(%q), attempting sudo...", path)
			items, sudoErr := globalSudoClient.ReadDir(path)
			if sudoErr == nil {
				vtui.DebugLog("VFS: Sudo ReadDir(%q) SUCCESS, items: %d", path, len(items))
				if len(items) > 0 && onChunk != nil {
					onChunk(items)
				}
				return nil
			}
			vtui.DebugLog("VFS: Sudo ReadDir(%q) FAILED: %v", path, sudoErr)
		} else {
			vtui.DebugLog("VFS: ReadDir(%q) FAILED: %v (Permission: %v, SudoAvailable: %v)", path, err, os.IsPermission(err), globalSudoClient.IsAvailable())
		}
		return err
	}
	defer f.Close()

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries, err := f.ReadDir(1000)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		items := make([]VFSItem, 0, len(entries))
		for _, e := range entries {
			info, _ := e.Info()
			var size int64
			var mtime time.Time
			var isExec bool
			if info != nil {
				size = info.Size()
				mtime = info.ModTime()
				isExec = info.Mode().Perm()&0111 != 0
			}
			isDir := e.IsDir()
			// If it's not a direct directory, it might be a symlink or a Windows Junction.
			// If it's not a regular file, ask the OS to resolve the final target.
			if !isDir && !e.Type().IsRegular() {
				if target, err := os.Stat(filepath.Join(path, e.Name())); err == nil {
					isDir = target.IsDir()
				}
			}

			items = append(items, VFSItem{
				Name:         e.Name(),
				Size:         size,
				IsDir:        isDir,
				MTime:        mtime,
				IsExecutable: isExec,
				IsHidden:     isHidden(filepath.Join(path, e.Name()), e.Name(), info),
			})
		}

		if len(items) > 0 && onChunk != nil {
			onChunk(items)
		}
	}
	return nil
}

func (v *OSVFS) Stat(ctx context.Context, path string) (VFSItem, error) {
	if ctx.Err() != nil {
		return VFSItem{}, ctx.Err()
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsPermission(err) && globalSudoClient.IsAvailable() {
			vtui.DebugLog("VFS: Permission denied for Stat(%q), attempting sudo...", path)
			item, sudoErr := globalSudoClient.Stat(path)
			if sudoErr == nil {
				vtui.DebugLog("VFS: Sudo Stat(%q) SUCCESS", path)
				return item, nil
			}
			vtui.DebugLog("VFS: Sudo Stat(%q) FAILED: %v", path, sudoErr)
		}
		return VFSItem{}, err
	}

	item := VFSItem{
		Name:         info.Name(),
		Size:         info.Size(),
		IsDir:        info.IsDir(),
		MTime:        info.ModTime(),
		UnixMode:     uint32(info.Mode().Perm()),
		IsExecutable: info.Mode().Perm()&0111 != 0,
		IsHidden:     isHidden(path, info.Name(), info),
	}

	// Platform specific time extraction
	fillPlatformTimes(&item, info)

	return item, nil
}

func (v *OSVFS) Join(elem ...string) string { return filepath.Join(elem...) }

func (v *OSVFS) Abs(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	// Correctly resolve relative to the VFS current path, not process CWD
	return filepath.Join(v.currentPath, path), nil
}

func (v *OSVFS) Base(path string) string { return filepath.Base(path) }
func (v *OSVFS) Dir(path string) string  { return filepath.Dir(path) }
func (v *OSVFS) MkDir(ctx context.Context, path string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err := os.MkdirAll(path, 0755)
	if err != nil && os.IsPermission(err) && globalSudoClient.IsAvailable() {
		vtui.DebugLog("VFS: Permission denied for MkDir(%q), attempting sudo...", path)
		return globalSudoClient.MkDir(path, 0755)
	}
	return err
}

func (v *OSVFS) Remove(ctx context.Context, path string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err := os.RemoveAll(path)
	if err != nil && os.IsPermission(err) && globalSudoClient.IsAvailable() {
		return globalSudoClient.Remove(path)
	}
	return err
}

func (v *OSVFS) Rename(ctx context.Context, old, new string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	err := os.Rename(old, new)
	if err != nil && os.IsPermission(err) && globalSudoClient.IsAvailable() {
		return globalSudoClient.Rename(old, new)
	}
	return err
}
func (v *OSVFS) SetAttributes(ctx context.Context, path string, item VFSItem) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Try native first
	errMode := os.Chmod(path, os.FileMode(item.UnixMode))

	var errOwn error
	if runtime.GOOS != "windows" {
		errOwn = os.Chown(path, item.Uid, item.Gid)
	}

	errTime := os.Chtimes(path, item.ATime, item.MTime)

	errPlat := applyPlatformAttributes(path, item)

	// If any operation failed due to permissions, try sudo
	if (os.IsPermission(errMode) || os.IsPermission(errOwn) || os.IsPermission(errTime) || os.IsPermission(errPlat)) && globalSudoClient.IsAvailable() {
		vtui.DebugLog("VFS: SetAttributes permission denied, trying sudo for %q", path)
		return globalSudoClient.SetAttributes(path, item)
	}

	if errMode != nil {
		return errMode
	}
	if errOwn != nil {
		return errOwn
	}
	if errTime != nil {
		return errTime
	}
	return errPlat
}

func (v *OSVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{
		HasServerSideCopy:  true,
		HasServerSideMove:  true,
		HasRandomAccess:    true,
		HasSearch:          false,
		HasUnixPermissions: runtime.GOOS != "windows",
	}
}

func (v *OSVFS) Search(ctx context.Context, path string, pattern string) (chan int64, error) {
	// OSVFS uses local streaming search implemented in actions.go
	return nil, nil
}

type osFileWrapper struct {
	*os.File
	size int64
}

func (f *osFileWrapper) Size() int64 { return f.size }
func (f *osFileWrapper) Read(ctx context.Context, p []byte) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return f.File.Read(p)
}

func (f *osFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return f.File.ReadAt(p, off)
}

func (v *OSVFS) Open(ctx context.Context, path string) (ReadAtCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsPermission(err) && globalSudoClient.IsAvailable() {
			vtui.DebugLog("VFS: Permission denied for Open(%q), attempting sudo...", path)
			sudoF, sudoErr := globalSudoClient.Open(path, os.O_RDONLY, 0)
			if sudoErr == nil {
				info, _ := sudoF.Stat()
				vtui.DebugLog("VFS: Sudo Open(%q) SUCCESS, size: %d", path, info.Size())
				return &osFileWrapper{File: sudoF, size: info.Size()}, nil
			}
			vtui.DebugLog("VFS: Sudo Open(%q) FAILED: %v", path, sudoErr)
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &osFileWrapper{File: f, size: info.Size()}, nil
}

func (v *OSVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f, err := os.Create(path)
	if err != nil && os.IsPermission(err) && globalSudoClient.IsAvailable() {
		vtui.DebugLog("VFS: Permission denied for Create(%q), attempting sudo...", path)
		return globalSudoClient.Open(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	}
	return f, err
}

func (v *OSVFS) ParentVFS() VFS {
	return nil // OSVFS is the root
}
func (v *OSVFS) Clone() VFS {
	return NewOSVFS(v.currentPath)
}
func (v *OSVFS) Close() error { return nil }
