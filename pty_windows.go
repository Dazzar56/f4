//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PTY для Windows реализован через ConPTY API (доступно в Windows 10+).
type PTY struct {
	mu           sync.Mutex
	console      windows.Handle
	inPipe       windows.Handle
	outPipe      windows.Handle
	process      *windows.ProcessInformation
	inWriter     *os.File
	outReader    *os.File
}

func NewPTY() (*PTY, error) {
	var inPipeOur, inPipePty windows.Handle
	var outPipeOur, outPipePty windows.Handle

	// Создаем пайпы для ввода-вывода
	if err := windows.CreatePipe(&inPipeOur, &inPipePty, nil, 0); err != nil {
		return nil, err
	}
	if err := windows.CreatePipe(&outPipePty, &outPipeOur, nil, 0); err != nil {
		return nil, err
	}

	// Создаем псевдоконсоль
	var console windows.Handle
	size := windows.Coord{X: 80, Y: 24}
	err := windows.CreatePseudoConsole(size, inPipePty, outPipePty, 0, &console)
	if err != nil {
		return nil, fmt.Errorf("failed to create pseudo console: %w (requires Windows 10+)", err)
	}

	return &PTY{
		console:   console,
		inPipe:    inPipeOur,
		outPipe:   outPipeOur,
		inWriter:  os.NewFile(uintptr(inPipeOur), "|in"),
		outReader: os.NewFile(uintptr(outPipeOur), "|out"),
	}, nil
}

func (p *PTY) Write(b []byte) (int, error) {
	return p.inWriter.Write(b)
}

func (p *PTY) Read(b []byte) (int, error) {
	return p.outReader.Read(b)
}

func (p *PTY) SetSize(cols, rows int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

func (p *PTY) Run(name string, args ...string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	cmdLine := windows.StringToUTF16Ptr(name)

	var attrList *windows.ProcThreadAttributeListContainer
	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil { return err }

	err = attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(p.console), unsafe.Sizeof(p.console))
	if err != nil { return err }

	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:    uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags: windows.STARTF_USESTDHANDLES,
		},
		AttributeList: attrList.List(),
	}

	pi := &windows.ProcessInformation{}
	flags := windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT

	err = windows.CreateProcess(nil, cmdLine, nil, nil, false, flags, nil, nil, &si.StartupInfo, pi)
	if err != nil {
		return err
	}

	p.process = pi
	return nil
}

func (p *PTY) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.process != nil {
		windows.TerminateProcess(p.process.Process, 0)
	}
	windows.ClosePseudoConsole(p.console)
	p.inWriter.Close()
	p.outReader.Close()
	return nil
}

func (p *PTY) Wait() error {
	if p.process == nil { return nil }
	_, err := windows.WaitForSingleObject(p.process.Process, windows.INFINITE)
	return err
}

func (p *PTY) IsBusy() bool {
	if p.process == nil { return false }
	var exitCode uint32
	err := windows.GetExitCodeProcess(p.process.Process, &exitCode)
	return err == nil && exitCode == 259 // STILL_ACTIVE
}

func GetSystemShell() string {
	return "cmd.exe"
}