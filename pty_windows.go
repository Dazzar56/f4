//go:build windows

package main

import "errors"

// PTY stub for Windows. Implementation via ConPTY will go here.
type PTY struct{}

func NewPTY() (*PTY, error) {
	return nil, errors.New("ConPTY implementation for Windows is pending")
}

func (p *PTY) Write(b []byte) (int, error)  { return 0, nil }
func (p *PTY) Read(b []byte) (int, error)   { return 0, nil }
func (p *PTY) SetSize(cols, rows int)       {}
func (p *PTY) Wait() error                  { return nil }
func (p *PTY) Run(name string, args ...string) error { return nil }

func GetSystemShell() string {
	return "cmd.exe"
}