package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/unxed/f4/sdk/f4rpc"
	"github.com/unxed/f4/vfs"
)

// RPCVFS acts as a local proxy that forwards VFS calls to the plugin process over RPC.
type RPCVFS struct {
	sess      *f4rpc.Session
	driveName string
	path      string
}

func NewRPCVFS(sess *f4rpc.Session, driveName string) *RPCVFS {
	return &RPCVFS{
		sess:      sess,
		driveName: driveName,
		path:      "/",
	}
}

func (v *RPCVFS) IsAtRoot() bool {
	return v.path == "/" || v.path == ""
}

func (v *RPCVFS) SetPath(p string) error {
	v.path = filepath.ToSlash(filepath.Clean(p))
	return nil
}

func (v *RPCVFS) GetPath() string {
	return filepath.FromSlash(v.path)
}

func (v *RPCVFS) Join(e ...string) string {
	return filepath.Join(e...)
}

func (v *RPCVFS) Abs(p string) (string, error) {
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Join(filepath.FromSlash(v.path), p), nil
}

func (v *RPCVFS) Base(p string) string {
	return filepath.Base(p)
}

func (v *RPCVFS) Dir(p string) string {
	return filepath.Dir(p)
}

func (v *RPCVFS) ReadDir(ctx context.Context, path string, onChunk func([]vfs.VFSItem)) error {
	var items []vfs.VFSItem
	req := map[string]string{"Drive": v.driveName, "Path": path}
	err := v.sess.Call("VFS.ReadDir", req, &items)
	if err == nil && len(items) > 0 {
		onChunk(items)
	}
	return err
}

func (v *RPCVFS) Stat(ctx context.Context, path string) (vfs.VFSItem, error) {
	var item vfs.VFSItem
	req := map[string]string{"Drive": v.driveName, "Path": path}
	err := v.sess.Call("VFS.Stat", req, &item)
	return item, err
}

func (v *RPCVFS) MkDir(ctx context.Context, p string) error {
	return fmt.Errorf("MkDir not implemented in RPC VFS yet")
}

func (v *RPCVFS) Remove(ctx context.Context, p string) error {
	return fmt.Errorf("Remove not implemented in RPC VFS yet")
}


func (v *RPCVFS) Rename(ctx context.Context, old, new string) error {
	return fmt.Errorf("Rename not implemented in RPC VFS yet")
}

func (v *RPCVFS) SetAttributes(ctx context.Context, path string, item vfs.VFSItem) error {
	return fmt.Errorf("SetAttributes not implemented in RPC VFS yet")
}

func (v *RPCVFS) GetCapabilities() vfs.VFSCapabilities {
	return vfs.VFSCapabilities{}
}

func (v *RPCVFS) Search(ctx context.Context, p, pat string) (chan int64, error) {
	return nil, nil
}

func (v *RPCVFS) Open(ctx context.Context, p string) (vfs.ReadAtCloser, error) {
	// For full support, this would ask the plugin for a file handle,
	// and proxy ReadAt calls. For now, we return permission denied to prevent crashes.
	return nil, os.ErrPermission
}

func (v *RPCVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	return nil, os.ErrPermission
}

func (v *RPCVFS) ParentVFS() vfs.VFS {
	return nil
}

func (v *RPCVFS) Close() error {
	return nil
}

func (v *RPCVFS) Clone() vfs.VFS {
	clone := NewRPCVFS(v.sess, v.driveName)
	clone.path = v.path
	return clone
}