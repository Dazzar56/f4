//go:build windows

// conptyprobe answers two questions about the ConPTY on the machine it runs
// on, for docs/TERMINAL_REFLOW.md §2–3:
//
//  1. Does CreatePseudoConsole accept PSEUDOCONSOLE_RESIZE_QUIRK (0x2), and
//     does it then stop re-emitting the whole viewport when resized?
//  2. Does a soft-wrapped line still arrive without a CRLF at the wrap point,
//     or did the 2024 ConPTY rewrite start inserting one?
//
// It starts its own hidden cmd.exe with the same calls f4 uses, types one
// line longer than the console, resizes, and writes the raw bytes it got to
// conptyprobe.log in the current directory. It does not touch f4.
//
// Build:  GOOS=windows go build ./tools/conptyprobe
// Run:    conptyprobe.exe
package main

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const resizeQuirk = 0x2

func main() {
	log, err := os.Create("conptyprobe.log")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer log.Close()
	out := func(format string, a ...any) {
		s := fmt.Sprintf(format, a...)
		fmt.Print(s)
		log.WriteString(s)
	}

	out("# conptyprobe %s\n", time.Now().Format(time.RFC3339))
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE); err == nil {
		build, _, _ := k.GetStringValue("CurrentBuild")
		ubr, _, _ := k.GetIntegerValue("UBR")
		name, _, _ := k.GetStringValue("ProductName")
		out("# build=%s.%d product=%s\n", build, ubr, name)
		k.Close()
	}

	for _, flags := range []uint32{0, resizeQuirk} {
		name := "default"
		if flags == resizeQuirk {
			name = "RESIZE_QUIRK"
		}
		out("\n===== flags=%#x (%s) =====\n", flags, name)
		if err := probe(flags, out); err != nil {
			out("FAILED: %v\n", err)
		}
	}
	out("\nDone. Please attach conptyprobe.log to the issue.\n")
}

func probe(flags uint32, out func(string, ...any)) error {
	var inPty, inOur, outOur, outPty windows.Handle
	if err := windows.CreatePipe(&inPty, &inOur, nil, 0); err != nil {
		return fmt.Errorf("CreatePipe(in): %w", err)
	}
	if err := windows.CreatePipe(&outOur, &outPty, nil, 0); err != nil {
		return fmt.Errorf("CreatePipe(out): %w", err)
	}
	var console windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 40, Y: 12}, inPty, outPty, flags, &console); err != nil {
		return fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	out("CreatePseudoConsole OK\n")
	windows.CloseHandle(inPty)
	windows.CloseHandle(outPty)
	defer windows.ClosePseudoConsole(console)

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, unsafe.Pointer(console), unsafe.Sizeof(console)); err != nil {
		return err
	}
	si := &windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attrs.List(),
	}
	pi := &windows.ProcessInformation{}
	if err := windows.CreateProcess(nil, windows.StringToUTF16Ptr("cmd.exe"), nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &si.StartupInfo, pi); err != nil {
		return fmt.Errorf("CreateProcess: %w", err)
	}
	defer windows.TerminateProcess(pi.Process, 0)

	writer := os.NewFile(uintptr(inOur), "|in")
	reader := os.NewFile(uintptr(outOur), "|out")
	defer writer.Close()
	defer reader.Close()

	got := make(chan []byte, 64)
	go func() {
		buf := make([]byte, 65536)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				got <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				close(got)
				return
			}
		}
	}()
	drain := func(d time.Duration) []byte {
		var all []byte
		deadline := time.After(d)
		for {
			select {
			case b, ok := <-got:
				if !ok {
					return all
				}
				all = append(all, b...)
			case <-deadline:
				return all
			}
		}
	}

	drain(1500 * time.Millisecond) // banner and first prompt
	writer.WriteString("echo ABCDEFGHIJ0123456789abcdefghij0123456789ABCDEFGHIJ0123456789\r")
	echoed := drain(800 * time.Millisecond)
	out("--- output of the wrapped echo (look for CRLF at the 40-column boundary) ---\n%s\n", escape(echoed))

	windows.ResizePseudoConsole(console, windows.Coord{X: 100, Y: 12})
	resized := drain(800 * time.Millisecond)
	out("--- output emitted by the resize to 100 columns (bytes=%d) ---\n", len(resized))
	out("--- few bytes = ConPTY left the buffer to us; a full screen = it repainted ---\n%s\n", escape(resized))

	writer.WriteString("exit\r")
	drain(300 * time.Millisecond)
	return nil
}

func escape(b []byte) string {
	var sb strings.Builder
	for _, c := range b {
		switch {
		case c == 0x1b:
			sb.WriteString("<ESC>")
		case c == '\r':
			sb.WriteString("<CR>")
		case c == '\n':
			sb.WriteString("<LF>\n")
		case c < 32:
			fmt.Fprintf(&sb, "<%d>", c)
		default:
			sb.WriteByte(c)
		}
	}
	return sb.String()
}
