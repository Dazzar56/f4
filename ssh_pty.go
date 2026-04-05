package main

import (
	"io"
	"golang.org/x/crypto/ssh"
)

// SSHPty реализует PtyBackend для удаленных сессий
type SSHPty struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func NewSSHPty(client *ssh.Client) (*SSHPty, error) {
	sess, err := client.NewSession()
	if err != nil {
		return nil, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	// Запрашиваем PTY для интерактивных приложений (top, mc, htop)
	if err := sess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		sess.Close()
		return nil, err
	}

	in, _ := sess.StdinPipe()
	out, _ := sess.StdoutPipe()

	return &SSHPty{
		client:  client,
		session: sess,
		stdin:   in,
		stdout:  out,
	}, nil
}

func (p *SSHPty) Read(b []byte) (int, error)  { return p.stdout.Read(b) }
func (p *SSHPty) Write(b []byte) (int, error) { return p.stdin.Write(b) }
func (p *SSHPty) Close() error                { return p.session.Close() }
func (p *SSHPty) SetSize(cols, rows int)      { p.session.WindowChange(rows, cols) }
func (p *SSHPty) IsBusy() bool                { return false } // Rely on pf.executing OSC trick
func (p *SSHPty) Wait() error                 { return p.session.Wait() }

func (p *SSHPty) Run(name string, args ...string) error {
	if name == "" {
		return p.session.Shell()
	}
	cmd := name
	for _, a := range args {
		cmd += " " + a
	}
	return p.session.Start(cmd)
}