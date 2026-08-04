package netfox

import (
	"net"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
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
func DialSSH(host, port, user, pass string, timeout int) (*ssh.Client, error) {
	auths := []ssh.AuthMethod{}

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentConn, err := net.Dial("unix", sock); err == nil {
			auths = append(auths, ssh.PublicKeysCallback(agent.NewClient(agentConn).Signers))
			defer agentConn.Close()
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         sshTimeout(timeout),
	}
	return ssh.Dial("tcp", host+":"+port, config)
}
