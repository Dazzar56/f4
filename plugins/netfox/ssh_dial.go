package netfox

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/unxed/f4/internal/netproxy"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshTimeout turns the timeout a site configuration carries into a duration,
// falling back to something sane when the field is empty or nonsense.
func sshTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// DialSSH opens an SSH connection the way every SSH based NetFox backend
// needs it: the agent first, then the usual private keys from ~/.ssh, then
// the password. It is shared by the SFTP and the FISH+ backends so that a
// site behaves identically whichever of them opens it.
func DialSSH(host, port, user, pass string, timeout int, px netproxy.Settings) (*ssh.Client, error) {
	hostKeyCallback, err := sshHostKeyCallback()
	if err != nil {
		return nil, err
	}

	auths := []ssh.AuthMethod{}
	var agentConn net.Conn

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if conn, err := net.Dial("unix", sock); err == nil {
			agentConn = conn
			agentClient := agent.NewClient(conn)
			auths = append(auths, ssh.PublicKeysCallback(agentClient.Signers))
		}
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
		HostKeyCallback: hostKeyCallback,
		Timeout:         sshTimeout(timeout),
	}
	// ssh.Dial would open the socket itself; going through netproxy instead
	// is what lets a site sit behind an HTTP CONNECT or SOCKS5 gateway.
	client, err := dialSSHVia(px, host+":"+port, config)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close() // Preserve the SSH dial failure.
		}
		return nil, err
	}
	if agentConn != nil {
		// The agent is used only for local authentication. Keeping its socket
		// open after the SSH handshake would make forwarding tempting and would
		// hold a needless connection to the user's agent for the whole session.
		_ = agentConn.Close()
	}
	return client, nil
}

func sshHostKeyCallback() (ssh.HostKeyCallback, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("SSH host-key verification: determine home directory: %w", err)
	}
	return sshHostKeyCallbackForHome(home)
}

func sshHostKeyCallbackForHome(home string) (ssh.HostKeyCallback, error) {
	if home == "" {
		return nil, fmt.Errorf("SSH host-key verification: home directory is empty")
	}

	knownHosts := make([]string, 0, 2)
	for _, name := range []string{"known_hosts", "known_hosts2"} {
		path := filepath.Join(home, ".ssh", name)
		info, err := os.Stat(path)
		switch {
		case err == nil && !info.IsDir():
			knownHosts = append(knownHosts, path)
		case err == nil:
			return nil, fmt.Errorf("SSH host-key verification: %s is a directory", path)
		case os.IsNotExist(err):
			continue
		default:
			return nil, fmt.Errorf("SSH host-key verification: inspect %s: %w", path, err)
		}
	}
	if len(knownHosts) == 0 {
		return nil, fmt.Errorf("SSH host-key verification: no ~/.ssh/known_hosts file found")
	}

	callback, err := knownhosts.New(knownHosts...)
	if err != nil {
		return nil, fmt.Errorf("SSH host-key verification: read known_hosts: %w", err)
	}
	return callback, nil
}

// dialSSHVia opens the transport through px and speaks SSH over it.
func dialSSHVia(px netproxy.Settings, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	ctx := context.Background()
	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}
	conn, err := px.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if config.Timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(config.Timeout))
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close() // Preserve the SSH handshake failure.
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}
