//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	modMsvcrtDLL            = syscall.NewLazyDLL("msvcrt.dll")
	procGetch               = modMsvcrtDLL.NewProc("_getch")
	kernel32SimpleExec      = syscall.NewLazyDLL("kernel32.dll")
	procReadConsoleOutputW  = kernel32SimpleExec.NewProc("ReadConsoleOutputW")
	procWriteConsoleOutputW = kernel32SimpleExec.NewProc("WriteConsoleOutputW")
)

type simpleCharInfo struct {
	UnicodeChar uint16
	Attributes  uint16
}

type simpleSmallRect struct {
	Left   int16
	Top    int16
	Right  int16
	Bottom int16
}

var (
	savedHostConsoleBuffer []simpleCharInfo
	savedHostConsoleW      int
	savedHostConsoleH      int
	savedHostConsoleMu     sync.Mutex
)

func modMsvcrtProcImpl() interface {
	Call(...uintptr) (uintptr, uintptr, error)
} {
	if procGetch.Find() == nil {
		return procGetch
	}
	return nil
}

func captureHostConsoleBufferImpl(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	hOut, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil || hOut == 0 || hOut == syscall.InvalidHandle {
		return
	}
	savedHostConsoleMu.Lock()
	defer savedHostConsoleMu.Unlock()
	size := w * h
	if len(savedHostConsoleBuffer) != size {
		savedHostConsoleBuffer = make([]simpleCharInfo, size)
	}
	savedHostConsoleW = w
	savedHostConsoleH = h

	bufSize := uintptr(uint32(uint16(w)) | (uint32(uint16(h)) << 16))
	bufCoord := uintptr(0)
	readRegion := simpleSmallRect{
		Left:   0,
		Top:    0,
		Right:  int16(w - 1),
		Bottom: int16(h - 1),
	}

	procReadConsoleOutputW.Call(
		uintptr(hOut),
		uintptr(unsafe.Pointer(&savedHostConsoleBuffer[0])),
		bufSize,
		bufCoord,
		uintptr(unsafe.Pointer(&readRegion)),
	)
}

func restoreHostConsoleBufferImpl() {
	savedHostConsoleMu.Lock()
	defer savedHostConsoleMu.Unlock()
	if len(savedHostConsoleBuffer) == 0 || savedHostConsoleW <= 0 || savedHostConsoleH <= 0 {
		return
	}
	hOut, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil || hOut == 0 || hOut == syscall.InvalidHandle {
		return
	}
	w, h := savedHostConsoleW, savedHostConsoleH
	bufSize := uintptr(uint32(uint16(w)) | (uint32(uint16(h)) << 16))
	bufCoord := uintptr(0)
	writeRegion := simpleSmallRect{
		Left:   0,
		Top:    0,
		Right:  int16(w - 1),
		Bottom: int16(h - 1),
	}

	procWriteConsoleOutputW.Call(
		uintptr(hOut),
		uintptr(unsafe.Pointer(&savedHostConsoleBuffer[0])),
		bufSize,
		bufCoord,
		uintptr(unsafe.Pointer(&writeRegion)),
	)
}

// hostConsoleBufferMatches reports whether the saved snapshot still describes a
// screen of the given size.
func hostConsoleBufferMatches(w, h int) bool {
	savedHostConsoleMu.Lock()
	defer savedHostConsoleMu.Unlock()
	return len(savedHostConsoleBuffer) > 0 && savedHostConsoleW == w && savedHostConsoleH == h
}
