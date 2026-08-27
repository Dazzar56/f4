//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// pty is the same set of calls f4 makes in cmd/f4/pty_windows.go, reduced to
// what a probe needs.
type pty struct {
	hpcon   syscall.Handle
	attrBuf []byte
	in      *os.File // we write here, the shell reads
	out     *os.File // the shell writes here, we read
	proc    syscall.Handle
	pid     uint32
	ch      chan []byte
	w, h    int16
	dead    bool
}

func newPTY(flags uint32, w, h int16, cmdline string) (*pty, error) {
	return newPTYProcess(flags, w, h, cmdline, nil, "")
}

// newPTYProcess is newPTY plus an isolated child environment and working
// directory. The exhaustive probe uses it to run every f4 reflow mode itself;
// the tester never has to set F4_WIN_REFLOW or VTUI_DEBUG by hand.
func newPTYProcess(flags uint32, w, h int16, cmdline string, overrides map[string]string, cwd string) (*pty, error) {
	var inPty, inOur, outOur, outPty syscall.Handle
	if err := syscall.CreatePipe(&inPty, &inOur, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe(in): %w", err)
	}
	if err := syscall.CreatePipe(&outOur, &outPty, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe(out): %w", err)
	}
	p := &pty{w: w, h: h}
	r, _, e := procCreatePseudoConsole.Call(
		coord{X: w, Y: h}.pack(),
		uintptr(inPty), uintptr(outPty), uintptr(flags),
		uintptr(unsafe.Pointer(&p.hpcon)))
	if r != 0 { // HRESULT: 0 == S_OK
		syscall.CloseHandle(inPty)
		syscall.CloseHandle(inOur)
		syscall.CloseHandle(outOur)
		syscall.CloseHandle(outPty)
		return nil, fmt.Errorf("CreatePseudoConsole hresult=%#x (%v)", uint32(r), e)
	}
	syscall.CloseHandle(inPty)
	syscall.CloseHandle(outPty)

	var size uintptr
	procInitAttrList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		size = 64
	}
	p.attrBuf = make([]byte, size)
	if ok, _, e := procInitAttrList.Call(uintptr(unsafe.Pointer(&p.attrBuf[0])), 1, 0,
		uintptr(unsafe.Pointer(&size))); ok == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList: %v", e)
	}
	// PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE is the odd one out: lpValue is the
	// HPCON itself, not the address of it. Passing &hpcon here creates the
	// process successfully and attaches it to nothing -- it starts, finds no
	// console, and exits without writing a byte.
	if ok, _, e := procUpdateAttr.Call(uintptr(unsafe.Pointer(&p.attrBuf[0])), 0,
		attrPseudoConsole, uintptr(p.hpcon), unsafe.Sizeof(p.hpcon), 0, 0); ok == 0 {
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %v", e)
	}

	var siex startupInfoEx
	siex.si.Cb = uint32(unsafe.Sizeof(siex))
	siex.attributeList = uintptr(unsafe.Pointer(&p.attrBuf[0]))
	var pi syscall.ProcessInformation
	cl := syscall.StringToUTF16(cmdline)
	creationFlags := uintptr(extendedStartupInfoPresent)
	var envBlock []uint16
	var envPtr uintptr
	if overrides != nil {
		envBlock = windowsEnvironmentBlock(os.Environ(), overrides)
		envPtr = uintptr(unsafe.Pointer(&envBlock[0]))
		creationFlags |= createUnicodeEnvironment
	}
	var cwdBuf []uint16
	var cwdPtr uintptr
	if cwd != "" {
		cwdBuf = syscall.StringToUTF16(cwd)
		cwdPtr = uintptr(unsafe.Pointer(&cwdBuf[0]))
	}
	if ok, _, e := procCreateProcessW.Call(0, uintptr(unsafe.Pointer(&cl[0])), 0, 0, 0,
		creationFlags, envPtr, cwdPtr,
		uintptr(unsafe.Pointer(&siex)), uintptr(unsafe.Pointer(&pi))); ok == 0 {
		return nil, fmt.Errorf("CreateProcess: %v", e)
	}
	runtime.KeepAlive(envBlock)
	runtime.KeepAlive(cwdBuf)
	syscall.CloseHandle(pi.Thread)
	p.proc = pi.Process
	p.pid = pi.ProcessId

	p.in = os.NewFile(uintptr(inOur), "|pty-in")
	p.out = os.NewFile(uintptr(outOur), "|pty-out")
	p.ch = make(chan []byte, 256)
	go func() {
		buf := make([]byte, 1<<16)
		for {
			n, err := p.out.Read(buf)
			if n > 0 {
				p.ch <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(p.ch)
				return
			}
		}
	}()
	return p, nil
}

// windowsEnvironmentBlock merges case-insensitively, sorts as CreateProcessW
// expects, and returns the required double-NUL-terminated UTF-16 block. An
// override value of "" deliberately removes a variable from the child.
func windowsEnvironmentBlock(base []string, overrides map[string]string) []uint16 {
	values := make(map[string]string, len(base)+len(overrides))
	names := make(map[string]string, len(base)+len(overrides))
	for _, entry := range base {
		i := strings.IndexByte(entry, '=')
		if i <= 0 {
			continue
		}
		name := entry[:i]
		key := strings.ToUpper(name)
		names[key] = name
		values[key] = entry[i+1:]
	}
	for name, value := range overrides {
		key := strings.ToUpper(name)
		if value == "" {
			delete(names, key)
			delete(values, key)
			continue
		}
		names[key] = name
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var block []uint16
	for _, key := range keys {
		line := syscall.StringToUTF16(names[key] + "=" + values[key])
		block = append(block, line...)
	}
	block = append(block, 0)
	return block
}

// drain collects output until the stream has been quiet for `quiet`, or until
// `max` has elapsed. Fixed sleeps lie about slow machines; quiet detection
// does not.
func (p *pty) drain(quiet, max time.Duration) []byte {
	b, _, _ := p.drainTimed(quiet, max)
	return b
}

// drainTimed also reports when the first and the last byte arrived, measured
// from the call. The first probe reported the oracle's cost as "509ms", which
// was just the quiet window: a measurement of its own timeout. These two are
// the honest numbers.
func (p *pty) drainTimed(quiet, max time.Duration) (data []byte, first, last time.Duration) {
	start := time.Now()
	deadline := start.Add(max)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return data, first, last
		}
		wait := quiet
		if left < wait {
			wait = left
		}
		timer := time.NewTimer(wait)
		select {
		case b, ok := <-p.ch:
			timer.Stop()
			if !ok {
				p.dead = true
				return data, first, last
			}
			if len(data) == 0 {
				first = time.Since(start)
			}
			data = append(data, b...)
			last = time.Since(start)
		case <-timer.C:
			return data, first, last
		}
	}
}

func (p *pty) send(s string) {
	if p.in != nil {
		p.in.WriteString(s)
	}
}

// line sends a command the way a user would: text plus CR.
func (p *pty) line(s string) { p.send(s + "\r") }

func (p *pty) resize(w, h int16) error {
	if w <= 0 || h <= 0 {
		return fmt.Errorf("refusing %dx%d (TERMINAL.md rule 4)", w, h)
	}
	r, _, e := procResizePseudoConsole.Call(uintptr(p.hpcon), coord{X: w, Y: h}.pack())
	if r != 0 {
		return fmt.Errorf("hresult=%#x (%v)", uint32(r), e)
	}
	p.w, p.h = w, h
	return nil
}

// exitCode reports the shell's exit code and whether it is still running.
// Used only when a session produced nothing, to say why.
func (p *pty) exitCode() (uint32, bool) {
	var code uint32
	r, _, _ := procGetExitCodeProcess.Call(uintptr(p.proc), uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return 0, false
	}
	return code, code == 259 // STILL_ACTIVE
}

func (p *pty) close() {
	// TERMINAL.md rule 1: the ConPTY handle closes before the pipes.
	if p.hpcon != 0 {
		procClosePseudoConsole.Call(uintptr(p.hpcon))
		p.hpcon = 0
	}
	if p.in != nil {
		p.in.Close()
	}
	if p.out != nil {
		p.out.Close()
	}
	if p.proc != 0 {
		syscall.TerminateProcess(p.proc, 0)
		syscall.CloseHandle(p.proc)
		p.proc = 0
	}
	if p.attrBuf != nil {
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&p.attrBuf[0])))
	}
}
