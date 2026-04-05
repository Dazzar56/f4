package vfs

import (
	"context"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/unxed/vtui"
)

type SFTPVFS struct {
	parent VFS
	client *sftp.Client
	ssh    *ssh.Client
	path   string
}

func NewSFTPVFS(parent VFS, host, port, user, pass string) (*SFTPVFS, error) {
	vtui.DebugLog("SFTP: Initiating connection to %s:%s (user: %s)", host, port, user)
	auths := []ssh.AuthMethod{}

	// 1. SSH Agent
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		vtui.DebugLog("SFTP: SSH_AUTH_SOCK found, adding agent auth")
		auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
		sshAgent.Close()
	}

	// 2. Keys from ~/.ssh (including encrypted)
	home, _ := os.UserHomeDir()
	for _, keyName := range []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
		keyPath := filepath.Join(home, ".ssh", keyName)
		if keyBytes, err := os.ReadFile(keyPath); err == nil {
			signer, err := ssh.ParsePrivateKey(keyBytes)
			if err != nil && pass != "" {
				// Try to decrypt if we have a password
				signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(pass))
			}
			if err == nil {
				vtui.DebugLog("SFTP: Found and loaded key: %s", keyName)
				auths = append(auths, ssh.PublicKeys(signer))
			}
		}
	}

	// 3. Password
	if pass != "" {
		vtui.DebugLog("SFTP: Adding password auth")
		auths = append(auths, ssh.Password(pass))
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            auths,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	// Диагностика DNS
	ips, _ := net.LookupIP(host)
	ipStrings := []string{}
	for _, ip := range ips { ipStrings = append(ipStrings, ip.String()) }
	vtui.DebugLog("SFTP: DNS Lookup for %s returned IPs: %v", host, ipStrings)

	addr := host + ":" + port
	vtui.DebugLog("SFTP: Dialing %s (tcp)...", addr)
	sshClient, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		vtui.DebugLog("SFTP: Connection failed: %v", err)
		return nil, err
	}
	vtui.DebugLog("SFTP: SSH handshake successful")
	
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

func (v *SFTPVFS) GetPath() string { return v.path }

func (v *SFTPVFS) SetPath(p string) error {
	if p == "" {
		p = "."
	}
	var target string
	if path.IsAbs(p) {
		target = p
	} else {
		target = v.Join(v.path, p)
	}
	target = path.Clean(target)
	info, err := v.client.Stat(target)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	v.path = target
	return nil
}

func (v *SFTPVFS) ReadDir(ctx context.Context, p string, onChunk func([]VFSItem)) error {
	entries, err := v.client.ReadDir(p)
	if err != nil {
		return err
	}

	var items []VFSItem
	for _, e := range entries {
		items = append(items, VFSItem{
			Name:         e.Name(),
			Size:         e.Size(),
			IsDir:        e.IsDir(),
			MTime:        e.ModTime(),
			IsExecutable: e.Mode().Perm()&0111 != 0,
		})
	}
	if len(items) > 0 {
		onChunk(items)
	}
	return nil
}

func (v *SFTPVFS) Stat(ctx context.Context, p string) (VFSItem, error) {
	info, err := v.client.Stat(p)
	if err != nil {
		return VFSItem{}, err
	}
	return VFSItem{
		Name:         info.Name(),
		Size:         info.Size(),
		IsDir:        info.IsDir(),
		MTime:        info.ModTime(),
		IsExecutable: info.Mode().Perm()&0111 != 0,
	}, nil
}

func (v *SFTPVFS) Join(elem ...string) string   { return path.Join(elem...) }
func (v *SFTPVFS) Abs(p string) (string, error) { return v.Join(v.path, p), nil }
func (v *SFTPVFS) Base(p string) string         { return path.Base(p) }
func (v *SFTPVFS) Dir(p string) string          { return path.Dir(p) }

func (v *SFTPVFS) MkDir(ctx context.Context, p string) error { return v.client.MkdirAll(p) }
func (v *SFTPVFS) Remove(ctx context.Context, p string) error {
	info, err := v.client.Stat(p)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return v.client.RemoveDirectory(p)
	}
	return v.client.Remove(p)
}
func (v *SFTPVFS) Rename(ctx context.Context, old, new string) error {
	return v.client.Rename(old, new)
}

func (v *SFTPVFS) GetCapabilities() VFSCapabilities {
	return VFSCapabilities{HasRandomAccess: true}
}
func (v *SFTPVFS) Search(ctx context.Context, p, pat string) (chan int64, error) { return nil, nil }

// sftpFileWrapper обеспечивает интерфейс ReadAtCloser для редактора f4
type sftpFileWrapper struct {
	*sftp.File
	size int64
}

func (w *sftpFileWrapper) Size() int64 { return w.size }
func (w *sftpFileWrapper) ReadAt(ctx context.Context, p []byte, off int64) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return w.File.ReadAt(p, off)
}
func (w *sftpFileWrapper) Read(ctx context.Context, p []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	return w.File.Read(p)
}

func (v *SFTPVFS) Open(ctx context.Context, p string) (ReadAtCloser, error) {
	f, err := v.client.Open(p)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &sftpFileWrapper{File: f, size: info.Size()}, nil
}

func (v *SFTPVFS) Create(ctx context.Context, p string) (io.WriteCloser, error) {
	return v.client.Create(p)
}

func (v *SFTPVFS) ParentVFS() VFS { return v.parent }
func (v *SFTPVFS) Close() error {
	if v.client != nil { v.client.Close() }
	if v.ssh != nil { return v.ssh.Close() }
	return nil
}
func (v *SFTPVFS) SSHClient() *ssh.Client { return v.ssh }