package vfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"
)

type diskFileWrapper struct {
	*os.File
	size int64
}

func (f *diskFileWrapper) Size() int64 { return f.size }
func (f *diskFileWrapper) Read(ctx context.Context, p []byte) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return f.File.Read(p)
}
func (f *diskFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (n int, err error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return f.File.ReadAt(p, off)
}

// DisksVFS is a specialized read-only virtual filesystem that lists
// physical block devices (PhysicalDriveX on Windows, /dev block devices on Unix)
// allowing the user to view and hex-edit raw disks.
type DisksVFS struct {
	*NullVFS
}

func NewDisksVFS() *DisksVFS {
	return &DisksVFS{NullVFS: NewNullVFS(0)}
}

func (v *DisksVFS) GetPath() string { return "disks://" }
func (v *DisksVFS) IsAbs(p string) bool { return true }
func (v *DisksVFS) IsAtRoot() bool { return true }
func (v *DisksVFS) Base(p string) string { return strings.TrimPrefix(p, "disks://") }
func (v *DisksVFS) Dir(p string) string { return "disks://" }
func (v *DisksVFS) Join(elem ...string) string {
	if len(elem) == 0 {
		return "disks://"
	}
	return "disks://" + elem[len(elem)-1]
}

func (v *DisksVFS) SetPath(p string) error {
	if p == "disks://" || p == "" || p == "/" {
		return nil
	}
	return os.ErrNotExist
}

func (v *DisksVFS) Clone() VFS {
	return NewDisksVFS()
}

func (v *DisksVFS) ReadDir(ctx context.Context, path string, onChunk func([]VFSItem)) error {
	var items []VFSItem
	if runtime.GOOS == "windows" {
		for i := 0; i < 64; i++ {
			if ctx.Err() != nil {
				break
			}
			name := fmt.Sprintf("PhysicalDrive%d", i)
			f, err := os.Open(prepareOSPath("\\\\.\\" + name))
			if err == nil {
				size, _ := f.Seek(0, io.SeekEnd)
				f.Close()
				if size > 0 {
					items = append(items, VFSItem{
						Name:      name,
						Size:      size,
						SizeKnown: true,
						MTime:     time.Now(),
					})
				}
			}
		}
	} else {
		entries, err := os.ReadDir("/dev")
		if err == nil {
			for _, e := range entries {
				if ctx.Err() != nil {
					break
				}
				info, err := e.Info()
				// Include block devices
				if err == nil && info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0 {
					f, err := os.Open("/dev/" + e.Name())
					size := int64(0)
					if err == nil {
						size, _ = f.Seek(0, io.SeekEnd)
						f.Close()
					}
					if size > 0 {
						items = append(items, VFSItem{
							Name:      e.Name(),
							Size:      size,
							SizeKnown: true,
							MTime:     info.ModTime(),
						})
					}
				}
			}
		}
	}
	if len(items) > 0 && onChunk != nil {
		onChunk(items)
	}
	return nil
}

func (v *DisksVFS) Stat(ctx context.Context, path string) (VFSItem, error) {
	name := v.Base(path)
	prefix := "/dev/"
	if runtime.GOOS == "windows" {
		prefix = "\\\\.\\"
	}
	f, err := os.Open(prepareOSPath(prefix + name))
	if err != nil {
		return VFSItem{}, err
	}
	size, _ := f.Seek(0, io.SeekEnd)
	f.Close()
	return VFSItem{Name: name, Size: size, SizeKnown: true, MTime: time.Now()}, nil
}

func (v *DisksVFS) Open(ctx context.Context, path string) (ReadAtCloser, error) {
	name := v.Base(path)
	prefix := "/dev/"
	if runtime.GOOS == "windows" {
		prefix = "\\\\.\\"
	}
	f, err := os.OpenFile(prepareOSPath(prefix+name), os.O_RDWR, 0)
	if err != nil {
		f, err = os.Open(prepareOSPath(prefix + name))
		if err != nil {
			return nil, err
		}
	}
	size, _ := f.Seek(0, io.SeekEnd)
	f.Seek(0, io.SeekStart)
	return &diskFileWrapper{File: f, size: size}, nil
}

func (v *DisksVFS) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("cannot create files on physical disks")
}

func (v *DisksVFS) PatchInPlace(ctx context.Context, path string, pieces []PatchPiece) error {
	name := v.Base(path)
	prefix := "/dev/"
	if runtime.GOOS == "windows" {
		prefix = "\\\\.\\"
	}
	f, err := os.OpenFile(prepareOSPath(prefix+name), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	var newOffset int64 = 0
	for _, p := range pieces {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if p.Data != nil {
			if _, err := f.WriteAt(p.Data, newOffset); err != nil {
				return err
			}
		} else {
			if p.Offset != newOffset {
				return fmt.Errorf("in-place patching requires unchanged pieces to remain at their original offsets (no insertions/deletions)")
			}
		}
		newOffset += p.Length
	}
	return nil
}
