//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"github.com/unxed/vtui"
	"golang.org/x/sys/windows"
)

var (
	modKernel32             = windows.NewLazySystemDLL("kernel32.dll")
	procCreatePseudoConsole = modKernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = modKernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = modKernel32.NewProc("ClosePseudoConsole")
)

// conPTYAvailable checks whether the Windows ConPTY API is available in kernel32.dll
// without panicking on older Windows versions (Windows 7, 8, 8.1, Server 2012/2016).
func conPTYAvailable() bool {
	if vtui.IsWine() {
		return false
	}
	return procCreatePseudoConsole.Find() == nil &&
		procResizePseudoConsole.Find() == nil &&
		procClosePseudoConsole.Find() == nil
}
func isPlatformPTYUsable() bool {
	return conPTYAvailable()
}

// PTY для Windows реализован через ConPTY API (доступно в Windows 10+).
type PTY struct {
	mu        sync.Mutex
	console   windows.Handle
	inPipe    windows.Handle
	outPipe   windows.Handle
	process   *windows.ProcessInformation
	inWriter  *os.File
	outReader *os.File

	lastBusyCheck time.Time
	lastBusyState bool

	Cmd *exec.Cmd //for interface compatability with Pty_unix, DO NOT USE
}

func NewPTY() (*PTY, error) {
	if !conPTYAvailable() {
		return nil, fmt.Errorf("ConPTY is not supported on this Windows version (requires Windows 10 build 1809+)")
	}

	var inPipeOur, inPipePty windows.Handle
	var outPipeOur, outPipePty windows.Handle

	// Создаем пайпы для ввода-вывода (CreatePipe: readHandle, writeHandle)
	// inPipe: PTY читает, мы пишем
	if err := windows.CreatePipe(&inPipePty, &inPipeOur, nil, 0); err != nil {
		return nil, err
	}
	// outPipe: мы читаем, PTY пишет
	if err := windows.CreatePipe(&outPipeOur, &outPipePty, nil, 0); err != nil {
		windows.CloseHandle(inPipePty)
		windows.CloseHandle(inPipeOur)
		return nil, err
	}

	// Создаем псевдоконсоль
	var console windows.Handle
	size := windows.Coord{X: 80, Y: 24}
	err := windows.CreatePseudoConsole(size, inPipePty, outPipePty, 0, &console)
	if err != nil {
		windows.CloseHandle(inPipePty)
		windows.CloseHandle(inPipeOur)
		windows.CloseHandle(outPipePty)
		windows.CloseHandle(outPipeOur)
		return nil, fmt.Errorf("failed to create pseudo console: %w (requires Windows 10+)", err)
	}

	// Закрываем наши копии хэндлов PTY, чтобы EOF корректно передавался при закрытии дочернего процесса
	windows.CloseHandle(inPipePty)
	windows.CloseHandle(outPipePty)

	return &PTY{
		console:   console,
		inPipe:    inPipeOur,
		outPipe:   outPipeOur,
		inWriter:  os.NewFile(uintptr(inPipeOur), "|in"),
		outReader: os.NewFile(uintptr(outPipeOur), "|out"),
	}, nil
}

func (p *PTY) Write(b []byte) (int, error) {
	vtui.DebugLog("PTY_WIN_TRACE: Writing %d bytes: %q", len(b), string(b))
	return p.inWriter.Write(b)
}

func (p *PTY) Read(b []byte) (int, error) {
	n, err := p.outReader.Read(b)
	if n > 0 {
		vtui.DebugLog("PTY_WIN_TRACE: Read %d bytes: %q", n, string(b[:n]))
	}
	return n, err
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
	if err != nil {
		return err
	}

	err = attrList.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(p.console), unsafe.Sizeof(p.console))
	if err != nil {
		return err
	}

	si := &windows.StartupInfoEx{
		StartupInfo: windows.StartupInfo{
			Cb:    uint32(unsafe.Sizeof(windows.StartupInfoEx{})),
			Flags: windows.STARTF_USESTDHANDLES,
		},
		ProcThreadAttributeList: attrList.List(),
	}

	pi := &windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT)

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
		windows.CloseHandle(p.process.Process)
		windows.CloseHandle(p.process.Thread)
		p.process = nil
	}
	windows.ClosePseudoConsole(p.console)
	p.inWriter.Close()
	p.outReader.Close()
	return nil
}

func (p *PTY) Wait() error {
	if p.process == nil {
		return nil
	}
	_, err := windows.WaitForSingleObject(p.process.Process, windows.INFINITE)
	return err
}

func (p *PTY) IsBusy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.process == nil {
		return false
	}

	// Increase cache timeout to 1 second to prevent high CPU usage
	// from CreateToolhelp32Snapshot during idle UI redraws.
	if time.Since(p.lastBusyCheck) < 1000*time.Millisecond {
		return p.lastBusyState
	}

	var exitCode uint32
	err := windows.GetExitCodeProcess(p.process.Process, &exitCode)
	if err != nil || exitCode != 259 { // 259 = STILL_ACTIVE
		p.lastBusyState = false
		p.lastBusyCheck = time.Now()
		return false
	}

	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		p.lastBusyState = false
		p.lastBusyCheck = time.Now()
		return false
	}
	defer windows.CloseHandle(snapshot)

	var pe32 windows.ProcessEntry32
	pe32.Size = uint32(unsafe.Sizeof(pe32))

	if err := windows.Process32First(snapshot, &pe32); err != nil {
		p.lastBusyState = false
		p.lastBusyCheck = time.Now()
		return false
	}

	for {
		if pe32.ParentProcessID == p.process.ProcessId {
			p.lastBusyState = true
			p.lastBusyCheck = time.Now()
			return true
		}
		if err := windows.Process32Next(snapshot, &pe32); err != nil {
			break
		}
	}

	p.lastBusyState = false
	p.lastBusyCheck = time.Now()
	return false
}

func GetSystemShell() string {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		return "cmd.exe"
	}
	return shell
}
