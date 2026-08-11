//go:build linux || darwin || freebsd

package fusefs

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/unxed/f4/vfs"
)

const supported = true

const (
	defaultAttrTimeout  = 3 * time.Second
	defaultEntryTimeout = 3 * time.Second
)

// startServer mounts the bridge. fs.Mount already waits for the kernel to
// acknowledge the mount, so a successful return means the directory is
// readable by other processes.
func startServer(ctx context.Context, m *Mount, opts Options) error {
	attrTimeout := opts.AttrTimeout
	if attrTimeout <= 0 {
		attrTimeout = defaultAttrTimeout
	}
	entryTimeout := opts.EntryTimeout
	if entryTimeout <= 0 {
		entryTimeout = defaultEntryTimeout
	}

	fsOpts := &fs.Options{
		AttrTimeout:  &attrTimeout,
		EntryTimeout: &entryTimeout,
		RootStableAttr: &fs.StableAttr{
			Ino: inodeOf(m.RootPath),
		},
	}
	fsOpts.MountOptions.FsName = m.Source
	fsOpts.MountOptions.Name = "f4"
	fsOpts.MountOptions.AllowOther = opts.AllowOther
	fsOpts.MountOptions.Debug = opts.Debug
	if opts.ReadOnly {
		fsOpts.MountOptions.Options = append(fsOpts.MountOptions.Options, "ro")
	}

	root := &node{b: m.bridge, path: m.RootPath}
	server, err := fs.Mount(m.MountPoint, root, fsOpts)
	if err != nil {
		return err
	}
	m.server = server
	return nil
}

// node is one object in the mounted tree, identified by its VFS-native
// path. Nothing else is cached on it: the bridge decides what is worth
// remembering.
type node struct {
	fs.Inode
	b    *bridge
	path string
}

var (
	_ = (fs.NodeGetattrer)((*node)(nil))
	_ = (fs.NodeLookuper)((*node)(nil))
	_ = (fs.NodeReaddirer)((*node)(nil))
	_ = (fs.NodeOpener)((*node)(nil))
	_ = (fs.NodeStatfser)((*node)(nil))
)

func (n *node) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	item, err := n.b.stat(ctx, n.path)
	if err != nil {
		return errnoOf(err)
	}
	fillAttr(&out.Attr, item, n.path)
	return 0
}

func (n *node) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	item, err := n.b.lookup(ctx, n.path, name)
	if err != nil {
		return nil, errnoOf(err)
	}
	childPath := n.b.join(n.path, name)
	fillAttr(&out.Attr, item, childPath)

	stable := fs.StableAttr{Ino: inodeOf(childPath), Mode: typeBits(item)}
	child := n.NewInode(ctx, &node{b: n.b, path: childPath}, stable)
	return child, 0
}

func (n *node) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	items, err := n.b.readDir(ctx, n.path)
	if err != nil {
		return nil, errnoOf(err)
	}
	entries := make([]fuse.DirEntry, 0, len(items))
	for _, item := range items {
		name := displayName(item.Name)
		if name == "" || name == "." || name == ".." {
			continue
		}
		entries = append(entries, fuse.DirEntry{
			Name: name,
			Mode: typeBits(item),
			Ino:  inodeOf(n.b.join(n.path, name)),
		})
	}
	return fs.NewListDirStream(entries), 0
}

func (n *node) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&uint32(syscall.O_ACCMODE) != uint32(syscall.O_RDONLY) {
		return nil, 0, syscall.EROFS
	}
	item, err := n.b.stat(ctx, n.path)
	if err != nil {
		return nil, 0, errnoOf(err)
	}
	if item.IsDir {
		return nil, 0, syscall.EISDIR
	}
	h, err := n.b.open(ctx, n.path, item.Size)
	if err != nil {
		return nil, 0, errnoOf(err)
	}
	return &fileHandle{h: h}, 0, 0
}

// Statfs reports something plausible rather than nothing: tools like df and
// some file dialogs treat a failing statfs as a broken file system.
func (n *node) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	out.Blocks = 0
	out.Bfree = 0
	out.Bavail = 0
	out.Bsize = 4096
	out.NameLen = 255
	return 0
}

// fileHandle adapts one open VFS reader to the FUSE file protocol.
type fileHandle struct {
	h *handle
}

var (
	_ = (fs.FileReader)((*fileHandle)(nil))
	_ = (fs.FileReleaser)((*fileHandle)(nil))
)

func (f *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	n, err := f.h.readAt(ctx, dest, off)
	if err != nil && n == 0 {
		return nil, errnoOf(err)
	}
	return fuse.ReadResultData(dest[:n]), 0
}

func (f *fileHandle) Release(ctx context.Context) syscall.Errno {
	f.h.release()
	return 0
}

func typeBits(item vfs.VFSItem) uint32 {
	if item.IsDir {
		return fuse.S_IFDIR
	}
	return fuse.S_IFREG
}

// fillAttr converts VFS metadata into kernel attributes. Backends which
// report neither ownership nor permissions are presented as read-only files
// belonging to whoever started f4, because a mount nobody can read is
// worse than an approximate one.
func fillAttr(out *fuse.Attr, item vfs.VFSItem, itemPath string) {
	out.Ino = inodeOf(itemPath)
	perm := unixMode(item) & 0o7777
	if item.IsDir {
		out.Mode = fuse.S_IFDIR | perm
		out.Nlink = 2
	} else {
		out.Mode = fuse.S_IFREG | perm
		out.Nlink = 1
		if item.Size > 0 {
			out.Size = uint64(item.Size)
			out.Blocks = (out.Size + 511) / 512
		}
	}

	mtime := item.MTime
	if mtime.IsZero() {
		mtime = time.Now()
	}
	atime, ctime := item.ATime, item.CTime
	if atime.IsZero() {
		atime = mtime
	}
	if ctime.IsZero() {
		ctime = mtime
	}
	out.SetTimes(&atime, &mtime, &ctime)

	if item.Uid > 0 {
		out.Uid = uint32(item.Uid)
	} else {
		out.Uid = uint32(os.Getuid())
	}
	if item.Gid > 0 {
		out.Gid = uint32(item.Gid)
	} else {
		out.Gid = uint32(os.Getgid())
	}
	out.Blksize = 4096
}

// errnoOf maps VFS errors onto errno values. Backends return plain errors,
// so anything unrecognized becomes EIO rather than a guess.
func errnoOf(err error) syscall.Errno {
	if err == nil {
		return 0
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	switch {
	case errors.Is(err, context.Canceled):
		return syscall.EINTR
	case errors.Is(err, context.DeadlineExceeded):
		return syscall.ETIMEDOUT
	case errors.Is(err, errClosed):
		return syscall.ENODEV
	case errors.Is(err, os.ErrNotExist):
		return syscall.ENOENT
	case errors.Is(err, os.ErrPermission):
		return syscall.EACCES
	case errors.Is(err, os.ErrExist):
		return syscall.EEXIST
	case errors.Is(err, os.ErrInvalid):
		return syscall.EINVAL
	}
	return syscall.EIO
}
