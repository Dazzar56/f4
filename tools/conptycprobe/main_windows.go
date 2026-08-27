//go:build windows

// conptycprobe is the measurement for docs/CONPTY_RESEARCH.md §8, alternative
// C. It keeps ConPTY much wider than the real terminal, prints known lines,
// changes only the height, and checks that each line remains one row in every
// repaint. It deliberately does not involve f4: this isolates the ConPTY
// premise before any integration work.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole        = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole        = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole         = kernel32.NewProc("ClosePseudoConsole")
	procInitAttrList               = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateAttr                 = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteAttrList             = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW             = kernel32.NewProc("CreateProcessW")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const (
	stdOutputHandle            = -11
	attributePseudoConsole     = 0x00020016
	extendedStartupInfoPresent = 0x00080000
)

type coord struct{ X, Y int16 }

func (c coord) pack() uintptr {
	return uintptr(uint32(uint16(c.X)) | uint32(uint16(c.Y))<<16)
}

type smallRect struct{ Left, Top, Right, Bottom int16 }

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

type startupInfoEx struct {
	si            syscall.StartupInfo
	attributeList uintptr
}

type lineCase struct{ text, name string }

type ptySession struct {
	console  syscall.Handle
	attrs    []byte
	in       *os.File
	out      *os.File
	process  syscall.Handle
	chunks   chan []byte
	stop     chan struct{}
	readDone chan struct{}
	cols     int16
	rows     int16
}

func getConsoleSize() (width, height int) {
	h, err := syscall.GetStdHandle(stdOutputHandle)
	if err != nil || h == syscall.InvalidHandle {
		return 0, 0
	}
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 || info.Window.Right < info.Window.Left || info.Window.Bottom < info.Window.Top {
		return 0, 0
	}
	return int(info.Window.Right-info.Window.Left) + 1, int(info.Window.Bottom-info.Window.Top) + 1
}

func newPTY(cols, rows int16) (*ptySession, error) {
	var inPTY, inParent, outParent, outPTY syscall.Handle
	if err := syscall.CreatePipe(&inPTY, &inParent, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe(input): %w", err)
	}
	if err := syscall.CreatePipe(&outParent, &outPTY, nil, 0); err != nil {
		syscall.CloseHandle(inPTY)
		syscall.CloseHandle(inParent)
		return nil, fmt.Errorf("CreatePipe(output): %w", err)
	}

	p := &ptySession{cols: cols, rows: rows}
	r, _, callErr := procCreatePseudoConsole.Call(
		coord{X: cols, Y: rows}.pack(), uintptr(inPTY), uintptr(outPTY), 0,
		uintptr(unsafe.Pointer(&p.console)))
	syscall.CloseHandle(inPTY)
	syscall.CloseHandle(outPTY)
	if r != 0 {
		syscall.CloseHandle(inParent)
		syscall.CloseHandle(outParent)
		return nil, fmt.Errorf("CreatePseudoConsole hresult=%#x (%v)", uint32(r), callErr)
	}

	var attrSize uintptr
	procInitAttrList.Call(0, 1, 0, uintptr(unsafe.Pointer(&attrSize)))
	if attrSize == 0 {
		p.closeConsole()
		syscall.CloseHandle(inParent)
		syscall.CloseHandle(outParent)
		return nil, fmt.Errorf("InitializeProcThreadAttributeList returned zero size")
	}
	p.attrs = make([]byte, attrSize)
	if ok, _, err := procInitAttrList.Call(uintptr(unsafe.Pointer(&p.attrs[0])), 1, 0,
		uintptr(unsafe.Pointer(&attrSize))); ok == 0 {
		p.closeConsole()
		syscall.CloseHandle(inParent)
		syscall.CloseHandle(outParent)
		return nil, fmt.Errorf("InitializeProcThreadAttributeList: %v", err)
	}
	if ok, _, err := procUpdateAttr.Call(uintptr(unsafe.Pointer(&p.attrs[0])), 0,
		attributePseudoConsole, uintptr(p.console), unsafe.Sizeof(p.console), 0, 0); ok == 0 {
		p.closeConsole()
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&p.attrs[0])))
		syscall.CloseHandle(inParent)
		syscall.CloseHandle(outParent)
		return nil, fmt.Errorf("UpdateProcThreadAttribute: %v", err)
	}

	var si startupInfoEx
	si.si.Cb = uint32(unsafe.Sizeof(si))
	si.attributeList = uintptr(unsafe.Pointer(&p.attrs[0]))
	cmdline := syscall.StringToUTF16("cmd.exe /Q /D")
	var pi syscall.ProcessInformation
	if ok, _, err := procCreateProcessW.Call(0, uintptr(unsafe.Pointer(&cmdline[0])), 0, 0, 0,
		uintptr(extendedStartupInfoPresent), 0, 0, uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi))); ok == 0 {
		p.closeConsole()
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&p.attrs[0])))
		syscall.CloseHandle(inParent)
		syscall.CloseHandle(outParent)
		return nil, fmt.Errorf("CreateProcess(cmd.exe): %v", err)
	}
	runtime.KeepAlive(cmdline)
	syscall.CloseHandle(pi.Thread)
	p.process = pi.Process
	p.in = os.NewFile(uintptr(inParent), "|conptyc-in")
	p.out = os.NewFile(uintptr(outParent), "|conptyc-out")
	p.chunks = make(chan []byte, 64)
	p.stop = make(chan struct{})
	p.readDone = make(chan struct{})
	go p.readLoop()
	return p, nil
}

func (p *ptySession) closeConsole() {
	if p.console != 0 {
		procClosePseudoConsole.Call(uintptr(p.console))
		p.console = 0
	}
}

func (p *ptySession) readLoop() {
	defer close(p.readDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := p.out.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			select {
			case p.chunks <- chunk:
			case <-p.stop:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *ptySession) close() {
	if p.stop != nil {
		select {
		case <-p.stop:
		default:
			close(p.stop)
		}
	}
	p.closeConsole()
	if p.in != nil {
		_ = p.in.Close()
	}
	if p.out != nil {
		_ = p.out.Close()
	}
	if p.process != 0 {
		_ = syscall.TerminateProcess(p.process, 0)
		_ = syscall.CloseHandle(p.process)
		p.process = 0
	}
	if p.readDone != nil {
		select {
		case <-p.readDone:
		case <-time.After(2 * time.Second):
		}
	}
	if len(p.attrs) != 0 {
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&p.attrs[0])))
		p.attrs = nil
	}
}

func (p *ptySession) send(text string) error {
	if p.in == nil {
		return fmt.Errorf("input pipe is closed")
	}
	data := []byte(text)
	for len(data) != 0 {
		n, err := p.in.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}

func (p *ptySession) line(text string) error { return p.send(text + "\r") }

func (p *ptySession) readUntil(needle string, timeout time.Duration) ([]byte, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	var data []byte
	for {
		if bytes.Contains(data, []byte(needle)) {
			return data, nil
		}
		select {
		case chunk := <-p.chunks:
			data = append(data, chunk...)
		case <-deadline.C:
			return data, fmt.Errorf("timeout waiting for %q after %v", needle, timeout)
		}
	}
}

func (p *ptySession) drain(quiet, maximum time.Duration) []byte {
	deadline := time.NewTimer(maximum)
	defer deadline.Stop()
	var data []byte
	for {
		timer := time.NewTimer(quiet)
		select {
		case chunk := <-p.chunks:
			timer.Stop()
			data = append(data, chunk...)
		case <-timer.C:
			return data
		case <-deadline.C:
			timer.Stop()
			return data
		}
	}
}

func (p *ptySession) resize(cols, rows int16) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid ConPTY size %dx%d", cols, rows)
	}
	r, _, err := procResizePseudoConsole.Call(uintptr(p.console), coord{X: cols, Y: rows}.pack())
	if r != 0 {
		return fmt.Errorf("ResizePseudoConsole(%d,%d) hresult=%#x (%v)", cols, rows, uint32(r), err)
	}
	p.cols, p.rows = cols, rows
	return nil
}

func markerLine(index, total int) lineCase {
	name := fmt.Sprintf("C_LINE_%02d", index)
	begin, end := name+"_BEGIN", name+"_END"
	if total < len(begin)+len(end) {
		total = len(begin) + len(end)
	}
	return lineCase{text: begin + strings.Repeat("X", total-len(begin)-len(end)) + end, name: name}
}

func escapeBytes(raw []byte, limit int) string {
	truncated := len(raw) > limit
	if truncated {
		raw = raw[:limit]
	}
	var b strings.Builder
	for _, c := range raw {
		switch c {
		case '\r':
			b.WriteString("\\r")
		case '\n':
			b.WriteString("\\n\n")
		case '\t':
			b.WriteString("\\t")
		case 0x1b:
			b.WriteString("\\x1b")
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "\\x%02x", c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	if truncated {
		b.WriteString("... [clipped]")
	}
	return b.String()
}

func checkLine(raw []byte, cols int, c lineCase) (markerObservation, int) {
	return inspectMarker(raw, c.text), lineRows(raw, cols, c.text)
}

func main() {
	defaultWidth, defaultHeight := getConsoleSize()
	if defaultWidth == 0 {
		defaultWidth = 120
	}
	if defaultHeight == 0 {
		defaultHeight = 25
	}
	wideWidth := flag.Int("width", 4000, "ConPTY width; 4000 is the Idea C measurement")
	height := flag.Int("height", defaultHeight, "ConPTY height; use the real terminal's row count")
	lineLength := flag.Int("line-length", 0, "first line length; default is longer than the detected terminal width")
	logPath := flag.String("log", "", "log path; default is conptyc-<timestamp>.log in the current directory")
	flag.Parse()

	if *wideWidth < 32 || *wideWidth > 8000 || *height < 4 || *height > 32766 {
		fmt.Fprintln(os.Stderr, "invalid size: width must be 32..8000 and height must be 4..32766")
		os.Exit(2)
	}
	if *logPath == "" {
		*logPath = filepath.Join(".", "conptyc-"+time.Now().Format("20060102-150405")+".log")
	}

	var log strings.Builder
	logf := func(format string, args ...any) { fmt.Fprintf(&log, format, args...) }
	logf("conptycprobe -- %s\n", time.Now().Format(time.RFC3339))
	logf("go=%s os=%s arch=%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	logf("detected_terminal=%dx%d requested_conpty=%dx%d\n", defaultWidth, defaultHeight, *wideWidth, *height)
	logf("idea=C keep ConPTY wide; resize height only; require one VT row per marker\n\n")

	pty, err := newPTY(int16(*wideWidth), int16(*height))
	if err != nil {
		logf("ERROR: %v\n", err)
		_ = finishLog(*logPath, log.String())
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pty.close()

	startup := pty.drain(500*time.Millisecond, 5*time.Second)
	logf("startup_bytes=%d raw=%s\n", len(startup), escapeBytes(startup, 4000))
	if err := pty.line("echo C_READY"); err != nil {
		logf("ERROR sending startup sentinel: %v\n", err)
		_ = finishLog(*logPath, log.String())
		os.Exit(1)
	}
	ready, err := pty.readUntil("C_READY", 10*time.Second)
	logf("ready_bytes=%d raw=%s\n", len(ready), escapeBytes(ready, 4000))
	if err != nil {
		logf("ERROR: %v\n", err)
		_ = finishLog(*logPath, log.String())
		os.Exit(1)
	}

	if err := pty.line("cls"); err != nil {
		logf("ERROR clearing the ConPTY screen: %v\n", err)
		_ = finishLog(*logPath, log.String())
		os.Exit(1)
	}
	_ = pty.drain(350*time.Millisecond, 3*time.Second)

	firstLength := *lineLength
	if firstLength == 0 {
		firstLength = defaultWidth + 64
		if firstLength < 120 {
			firstLength = 120
		}
	}
	if firstLength >= *wideWidth-16 {
		firstLength = *wideWidth / 2
	}
	secondLength := *wideWidth - 32
	lines := []lineCase{markerLine(0, firstLength), markerLine(1, secondLength)}
	allPassed := true

	logf("lines=%d first_length=%d second_length=%d (wide width=%d)\n", len(lines), len(lines[0].text), len(lines[1].text), *wideWidth)
	for _, c := range lines {
		if err := pty.line("echo " + c.text); err != nil {
			logf("ERROR sending %s: %v\n", c.name, err)
			allPassed = false
			continue
		}
		suffix := c.text[len(c.text)-len(c.name)-4:]
		chunk, readErr := pty.readUntil(suffix, 10*time.Second)
		obs, rows := checkLine(chunk, *wideWidth, c)
		passed := readErr == nil && obs.whole && !obs.split && rows == 1
		if !passed {
			allPassed = false
		}
		logf("initial.%s bytes=%d read_error=%v whole=%v split=%v rows=%d pass=%v raw=%s\n",
			c.name, len(chunk), readErr, obs.whole, obs.split, rows, passed, escapeBytes(chunk, 8000))
	}
	_ = pty.drain(350*time.Millisecond, 3*time.Second)

	resizeHeights := []int16{int16(*height - 1), int16(*height), int16(*height + 1), int16(*height)}
	for i, h := range resizeHeights {
		if err := pty.resize(int16(*wideWidth), h); err != nil {
			logf("resize.%d target=%dx%d ERROR=%v\n", i, *wideWidth, h, err)
			allPassed = false
			continue
		}
		frame := pty.drain(600*time.Millisecond, 5*time.Second)
		framePassed := len(frame) != 0
		logf("resize.%d target=%dx%d bytes=%d\n", i, *wideWidth, h, len(frame))
		for _, c := range lines {
			obs, rows := checkLine(frame, *wideWidth, c)
			passed := obs.whole && !obs.split && rows == 1
			if !passed {
				framePassed = false
			}
			logf("resize.%d.%s whole=%v split=%v rows=%d pass=%v\n", i, c.name, obs.whole, obs.split, rows, passed)
		}
		if !framePassed {
			allPassed = false
		}
		logf("resize.%d.raw=%s\n", i, escapeBytes(frame, 50000))
	}

	post := markerLine(2, firstLength)
	if err := pty.line("echo " + post.text); err != nil {
		logf("post_resize.ERROR=%v\n", err)
		allPassed = false
	} else {
		suffix := post.text[len(post.text)-len(post.name)-4:]
		chunk, readErr := pty.readUntil(suffix, 10*time.Second)
		obs, rows := checkLine(chunk, *wideWidth, post)
		passed := readErr == nil && obs.whole && !obs.split && rows == 1
		if !passed {
			allPassed = false
		}
		logf("post_resize.%s bytes=%d read_error=%v whole=%v split=%v rows=%d pass=%v raw=%s\n",
			post.name, len(chunk), readErr, obs.whole, obs.split, rows, passed, escapeBytes(chunk, 8000))
	}

	if allPassed {
		logf("\nVERDICT=PASS all markers stayed on one row through height-only repaints\n")
		fmt.Println("PASS: wide ConPTY delivered intact one-row markers through height-only repaints")
	} else {
		logf("\nVERDICT=FAIL inspect the raw frames above\n")
		fmt.Println("FAIL: see the log for the first split, missing, or malformed marker")
	}
	if err := finishLog(*logPath, log.String()); err != nil {
		fmt.Fprintln(os.Stderr, "could not write log:", err)
		os.Exit(1)
	}
	fmt.Println("Log:", *logPath)
	if !allPassed {
		os.Exit(2)
	}
}

func finishLog(path, content string) error { return os.WriteFile(path, []byte(content), 0644) }
