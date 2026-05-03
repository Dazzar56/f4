//go:build windows

package vfs

import (
	"errors"
	"net"
	"os"
)

var ErrSudoNotSupported = errors.New("sudo privilege elevation is not yet supported on Windows")

func sendMsg(conn *net.UnixConn, msg any, fd int) error {
	return ErrSudoNotSupported
}

func recvMsg(conn *net.UnixConn, msg any) (*os.File, error) {
	return nil, ErrSudoNotSupported
}
