package netfox

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
)

type SFTPVFS struct {
	parent vfs.VFS
	client *sftp.Client
	ssh    *ssh.Client
	path   string
}

func NewSFTPVFS(parent vfs.VFS, host, port, user, pass string) (*SFTPVFS, error) {
	vtui.DebugLog("NET: Initiating SFTP connection to %s:%s (user: %s)", host, port, user)
	auths := []ssh.AuthMethod{}

	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
		sshAgent.Close()
	}

	home, _ := os.UserHomeDir()
	for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
		keyPath := filepath.Join(home, ".ssh", keyName)
		if keyBytes, err := os.ReadFile(keyPath); err == nil {
			signer, err := ssh.ParsePrivateKey(keyBytes)
			if err != nil && pass != "" {
				signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(pass))
			}
			if err == nil {
				auths = append(auths, ssh.PublicKeys(signer))
			}
		}
	}

	if pass != "" {
		auths = append(auths, ssh.Password(pass))
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := host + ":" + port
	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, err
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, err
	}

	pwd, err := sftpClient.Getwd()
	if err != nil || pwd == "" {
		pwd = "/"
	}

	return &SFTPVFS{
		parent: parent,
		client: sftpClient,
		ssh:    sshClient,
		path:   pwd,
	}, nil
}

func (v *SFTPVFS) IsAtRoot() bool { return v.path == "/" || v.path == "" }
func (v *SFTPVFS) GetPath() string { return v.path }
func (v *SFTPVFS) SetPath(p string) error {
	var target string
	if path.IsAbs(p) { target = p } else { target = v.Join(v.path, p) }
	target = path.Clean(target)
	info, err := v.client.Stat(target)
	if err != nil { return err }
	if !info.IsDir() { return os.ErrInvalid }
	v.path = target
	return nil
}

func (v *SFTPVFS) ReadDir(ctx context.Context, p string, onChunk func([]vfs.VFSItem)) error {
	entries, err := v.client.ReadDir(p)
	if err != nil { return err }
	var items []vfs.VFSItem
	for _, e := range entries {
		items = append(items, vfs.VFSItem{
			Name: e.Name(), Size: e.Size(), IsDir: e.IsDir(),
			MTime: e.ModTime(), IsExecutable: e.Mode().Perm()&0111 != 0,
		})
	}
	onChunk(items)
	return nil
}

func (v *SFTPVFS) Stat(ctx context.Context, p string) (vfs.VFSItem, error) {
	info, err := v.client.Stat(p)
	if err != nil { return vfs.VFSItem{}, err }
	return vfs.VFSItem{
		Name: info.Name(), Size: info.Size(), IsDir: info.IsDir(),
		MTime: info.ModTime(), IsExecutable: info.Mode().Perm()&0111 != 0,
	}, nil
}

func (v *SFTPVFS) Join(e ...string) string { return path.Join(e...) }
func (v *SFTPVFS) Abs(p string) (string, error) { return v.Join(v.path, p), nil }
func (v *SFTPVFS) Base(p string) string { return path.Base(p) }
func (v *SFTPVFS) Dir(p string) string { return path.Dir(p) }
func (v *SFTPVFS) MkDir(ctx context.Context, p string) error { return v.client.MkdirAll(p) }
func (v *SFTPVFS) Remove(ctx context.Context, p string) error {
	info, err := v.client.Stat(p)
	if err != nil { return err }
	if info.IsDir() { return v.client.RemoveDirectory(p) }
	return v.client.Remove(p)
}
func (v *SFTPVFS) Rename(ctx context.Context, o, n string) error { return v.client.Rename(o, n) }
func (v *SFTPVFS) GetCapabilities() vfs.VFSCapabilities { return vfs.VFSCapabilities{HasRandomAccess: true} }
func (v *SFTPVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

func (v *SFTPVFS) Open(ctx context.Context, p string) (vfs.ReadAtCloser, error) {
	f, err := v.client.Open(p)
	if err != nil { return nil, err }
	info, err := f.Stat()
	if err != nil { f.Close(); return nil, err }
	return &sftpFileWrapper{File: f, size: info.Size()}, nil
}

func (v *SFTPVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) { return v.client.Create(p) }
func (v *SFTPVFS) ParentVFS() vfs.VFS { return v.parent }
func (v *SFTPVFS) Close() error {
	if v.client != nil { v.client.Close() }
	if v.ssh != nil { return v.ssh.Close() }
	return nil
}
func (v *SFTPVFS) Clone() vfs.VFS {
	// Re-auth is complex; return self reference.
	return v
}

func (v *SFTPVFS) OpenPty(cols, rows int) (any, error) {
	pty, err := NewSSHPty(v.ssh)
	if err != nil { return nil, err }
	pty.SetSize(cols, rows)
	pty.Run("")
	return pty, nil
}
type sftpProvider struct{}
func (p *sftpProvider) Name() string  { return "NetFox-SFTP" }
func (p *sftpProvider) Priority() int { return 100 }
func (p *sftpProvider) CanOpen(ctx context.Context, parent vfs.VFS, pth string) bool {
	w, ok := parent.(*netFoxVFSWrapper)
	if !ok { return false }
	item, err := w.Stat(ctx, pth)
	if err != nil || item.IsDir { return false }
	f, err := w.Open(ctx, pth)
	if err != nil { return false }
	defer f.Close()
	var cfg NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	return cfg.Type == "sftp" || cfg.Type == ""
}
func (p *sftpProvider) Open(ctx context.Context, parent vfs.VFS, pth string) (vfs.VFS, error) {
	w := parent.(*netFoxVFSWrapper)
	f, _ := w.Open(ctx, pth)
	defer f.Close()
	var cfg NetFoxConfig
	json.NewDecoder(ctxReader{f, ctx}).Decode(&cfg)
	port := cfg.Port
	if port == "" { port = "22" }
	return NewSFTPVFS(parent, cfg.Host, port, cfg.User, cfg.Pass)
}

type sftpProtocolHandler struct{}

func (ph *sftpProtocolHandler) Prefix() string      { return "sftp" }
func (ph *sftpProtocolHandler) DefaultPort() string { return "22" }
func (ph *sftpProtocolHandler) BuildExtraUI(cfg *NetFoxConfig, x, y, w, h int) (vtui.UIElement, func()) {
	return nil, func() {}
}

func init() {
	vfs.RegisterProvider(&sftpProvider{})
	RegisterProtocol(&sftpProtocolHandler{})
}

type sftpFileWrapper struct {
	*sftp.File
	size int64
}
func (w *sftpFileWrapper) Size() int64 { return w.size }
func (w *sftpFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	return w.File.ReadAt(p, off)
}
func (w *sftpFileWrapper) Read(ctx context.Context, p []byte) (int, error) {
	return w.File.Read(p)
}