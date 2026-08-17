//go:build windows

package main

import "syscall"

var (
	modMsvcrtDLL = syscall.NewLazyDLL("msvcrt.dll")
	procGetch    = modMsvcrtDLL.NewProc("_getch")
)

func modMsvcrtProcImpl() interface {
	Call(...uintptr) (uintptr, uintptr, error)
} {
	if procGetch.Find() == nil {
		return procGetch
	}
	return nil
}
