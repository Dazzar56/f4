package main

import "io"

// PtyBackend abstracts the difference between Unix PTY and Windows ConPTY.
type PtyBackend interface {
	io.ReadWriter
	io.Closer
	SetSize(cols, rows int)
	Wait() error
	Run(name string, args ...string) error
}