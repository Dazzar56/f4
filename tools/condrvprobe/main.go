//go:build windows

// condrvprobe answers one question, and records what the answer depends on.
//
// Every approach to reflow under Windows in docs/CONPTY_RESEARCH.md gropes
// for the console's wrap flag from outside the process that owns it. The
// flag exists -- conhost's ROW::wrapForced -- but no public API exports it,
// which is why winpty, WezTerm and f4 all ended up guessing (section 7).
//
// Direction D says: own the console server. Direction D2 says something
// cheaper: do not reimplement conhost, sit in front of it. The console API
// arrives at the server as messages over \Device\ConDrv\Server -- "client 4
// called WriteConsoleW with these characters", "client 4 asked for the
// screen buffer info". Those messages carry the application's *intent*,
// before any wrapping happens. A program that writes 185 characters into a
// 120-column buffer is unambiguously one logical line; nothing has to be
// inferred from ESC[K, and nothing can be wrong about it.
//
// This probe measures whether that seat is available to a normal program:
//
//  1. Can we create the server endpoint at all, unprivileged?
//  2. Does the driver hand us API messages, and what do their first bytes
//     look like? The layout is undocumented (microsoft/terminal#10463), so
//     the raw header is recorded for comparison across builds.
//  3. Does the system conhost accept a server handle we created, via the
//     documented-by-existence `--server <handle>` route? If it does, the
//     handle we hold is the real thing and a proxy is a matter of
//     forwarding, not of privilege.
//
// It changes nothing on the machine: it creates a console endpoint, talks to
// it, and closes it.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	ntdll                   = syscall.NewLazyDLL("ntdll.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	version                 = syscall.NewLazyDLL("version.dll")
	procNtCreateFile        = ntdll.NewProc("NtCreateFile")
	procRtlInitUnicodeStr   = ntdll.NewProc("RtlInitUnicodeString")
	procRtlGetVersion       = ntdll.NewProc("RtlGetVersion")
	procDeviceIoControl     = kernel32.NewProc("DeviceIoControl")
	procCreateFileW         = kernel32.NewProc("CreateFileW")
	procGetFileVersionInfo  = version.NewProc("GetFileVersionInfoW")
	procVerQueryValue       = version.NewProc("VerQueryValueW")
	procGetFileVersionSize  = version.NewProc("GetFileVersionInfoSizeW")
	procCreateEventW        = kernel32.NewProc("CreateEventW")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

type unicodeString struct {
	Length        uint16
	MaximumLength uint16
	Buffer        *uint16
}

type objectAttributes struct {
	Length                   uint32
	RootDirectory            uintptr
	ObjectName               *unicodeString
	Attributes               uint32
	SecurityDescriptor       uintptr
	SecurityQualityOfService uintptr
}

type ioStatusBlock struct {
	Status      uintptr
	Information uintptr
}

type osVersionInfoEx struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformId        uint32
	CSDVersion        [128]uint16
	ServicePackMajor  uint16
	ServicePackMinor  uint16
	SuiteMask         uint16
	ProductType       byte
	Reserved          byte
}

// The ConDrv control codes, from microsoft/terminal dep/Console/condrv.h
// (MIT). FILE_DEVICE_CONSOLE is 0x50 -- the first run of this probe used
// 0x8000 and every call came back "Incorrect function", which was the probe
// being wrong, not the driver refusing. The arithmetic is checkable against
// published numbers: FireEye's 2017 analysis names 0x50000F and 0x500013 for
// the input-read and output-write codes, and those are exactly functions 3
// and 4 with METHOD_NEITHER under device 0x50.
func ctlCode(function, method uint32) uint32 {
	return 0x50<<16 | function<<2 | method
}

const (
	methodOutDirect = 2
	methodNeither   = 3
)

var (
	ioctlReadIO     = ctlCode(1, methodOutDirect) // 0x500006
	ioctlSetSrvInfo = ctlCode(7, methodNeither)   // 0x50001F
	ioctlGetSrvPID  = ctlCode(8, methodNeither)   // 0x500023
)

// serverInformation is CD_IO_SERVER_INFORMATION: the event the driver
// signals when a message is waiting. The server must hand this over before
// it may read, which is the other half of what the first run got wrong.
type serverInformation struct {
	InputAvailableEvent uintptr
}

var out *os.File

func say(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	fmt.Print(line)
	if out != nil {
		out.WriteString(line)
	}
}

func main() {
	exe, _ := os.Executable()
	path := filepath.Join(filepath.Dir(exe), "condrvprobe-report.txt")
	if f, err := os.Create(path); err == nil {
		out = f
		defer f.Close()
	}

	say("=== condrvprobe 1 (issue #425: can f4 sit in the console-server seat?) ===\n")
	say("time: %s\n", time.Now().Format("2006-01-02 15:04:05 -0700"))
	say("report file: %s\n\n", path)

	reportEnvironment()
	h := reportServerEndpoint()
	if h != 0 {
		defer syscall.CloseHandle(syscall.Handle(h))
		reportMessages(h)
		reportConhostAcceptance(h)
	}

	say("\n--- Done ---\n")
	say("Please attach %s to the issue.\n", path)
	fmt.Print("\nPress Enter to close...")
	fmt.Scanln()
}

// reportEnvironment records what the answers below are answers *about*. The
// ConDrv protocol is undocumented, so every result here is only true for the
// build that produced it until another build says the same.
func reportEnvironment() {
	say("--- Where this is running ---\n")
	var vi osVersionInfoEx
	vi.OSVersionInfoSize = uint32(unsafe.Sizeof(vi))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	say("windows build: %d.%d.%d\n", vi.MajorVersion, vi.MinorVersion, vi.BuildNumber)

	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	for _, f := range []string{
		filepath.Join(sys, `System32\drivers\condrv.sys`),
		filepath.Join(sys, `System32\conhost.exe`),
	} {
		say("%s: %s\n", filepath.Base(f), fileVersion(f))
	}
	say("WT_SESSION: %s\n", orNone(os.Getenv("WT_SESSION")))
	say("\n")
}

func orNone(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

// reportServerEndpoint answers question 1: can an ordinary program create
// the endpoint a console server owns?
func reportServerEndpoint() uintptr {
	say("--- Question 1: can we create \\Device\\ConDrv\\Server? ---\n")
	say("This is the endpoint conhost is given when ConPTY starts it with\n")
	say("--server. If an unprivileged program cannot create one, direction D2\n")
	say("is closed before it starts.\n")

	name := `\Device\ConDrv\Server`
	var us unicodeString
	p, _ := syscall.UTF16PtrFromString(name)
	procRtlInitUnicodeStr.Call(uintptr(unsafe.Pointer(&us)), uintptr(unsafe.Pointer(p)))

	oa := objectAttributes{ObjectName: &us, Attributes: 0x00000002 /* OBJ_INHERIT */}
	oa.Length = uint32(unsafe.Sizeof(oa))

	var handle uintptr
	var iosb ioStatusBlock
	const (
		genericAllAccess = 0x10000000
		fileShareRW      = 0x00000003
		fileCreate       = 0x00000002
		syncNonAlert     = 0x00000020
	)
	st, _, _ := procNtCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)),
		genericAllAccess|0x00100000, // | SYNCHRONIZE
		uintptr(unsafe.Pointer(&oa)),
		uintptr(unsafe.Pointer(&iosb)),
		0, 0, fileShareRW, fileCreate, syncNonAlert, 0, 0,
	)
	if st != 0 {
		say("RESULT: NtCreateFile failed, NTSTATUS 0x%08X\n", uint32(st))
		say("        (0xC0000022 is access denied; 0xC0000034 means the name is\n")
		say("         not there, which would mean this Windows does not use the\n")
		say("         ConDrv architecture at all.)\n")
		say("VERDICT: the server seat is NOT available to a normal program here.\n\n")
		return 0
	}
	say("RESULT: created, handle 0x%X\n", handle)
	say("VERDICT: the server seat IS available unprivileged on this build.\n\n")
	return handle
}

// reportMessages answers question 2: does the driver deliver API messages?
//
// The order matters and the first run did not follow it. A server must
// announce itself with SET_SERVER_INFORMATION, handing the driver an event
// to signal, before READ_IO means anything. This does that, then waits on
// the event rather than blocking in the ioctl, so a machine with no client
// attached gives a clean "nothing yet" instead of a hang.
func reportMessages(server uintptr) {
	say("--- Question 2: does the driver deliver API messages? ---\n")
	say("A message is what a client's console call arrives as: which client,\n")
	say("which API, and its payload. That payload is the application's intent\n")
	say("before any wrapping -- the fact every approach so far had to guess.\n")
	say("control codes: READ_IO=0x%06X SET_SERVER_INFORMATION=0x%06X GET_SERVER_PID=0x%06X\n",
		ioctlReadIO, ioctlSetSrvInfo, ioctlGetSrvPID)

	ev, err := createEvent()
	if err != nil {
		say("RESULT: could not create the signalling event: %v\n\n", err)
		return
	}
	defer syscall.CloseHandle(syscall.Handle(ev))

	info := serverInformation{InputAvailableEvent: ev}
	var returned uint32
	r, _, err := procDeviceIoControl.Call(server, uintptr(ioctlSetSrvInfo),
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0, 0,
		uintptr(unsafe.Pointer(&returned)), 0)
	if r == 0 {
		say("SET_SERVER_INFORMATION failed: %v\n", err)
		say("VERDICT: we hold the endpoint but may not act as its server. That\n")
		say("         closes the proxy shape of D2 on this build; record it.\n\n")
		return
	}
	say("SET_SERVER_INFORMATION: accepted -- the driver took our event, so we\n")
	say("are this endpoint's server.\n")

	// A client has to exist before anything is waiting. Attach one to *our*
	// server: a console process launched so that it connects here.
	client, cleanup := launchClient(server)
	if cleanup != nil {
		defer cleanup()
	}
	say("client launch: %s\n", client)

	const waitObject0 = 0
	w, _, _ := procWaitForSingleObject.Call(ev, 3000)
	if w != waitObject0 {
		say("RESULT: no message within 3s (wait returned 0x%X).\n", w)
		say("        With a client attached this would mean the driver routes\n")
		say("        its calls elsewhere; with none, it is simply quiet.\n\n")
		return
	}

	buf := make([]byte, 4096)
	r, _, err = procDeviceIoControl.Call(server, uintptr(ioctlReadIO),
		0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&returned)), 0)
	if r == 0 {
		say("RESULT: the event fired but READ_IO failed: %v\n\n", err)
		return
	}
	say("RESULT: a message arrived, %d bytes.\n", returned)
	n := int(returned)
	if n > 64 {
		n = 64
	}
	say("first %d bytes: % x\n", n, buf[:n])
	say("(record these: the layout is undocumented -- microsoft/terminal#10463 --\n")
	say(" so the only evidence it is stable is the same shape on another build.)\n\n")
}

// launchClient starts a console program attached to our server endpoint, so
// that question 2 has something to observe. A client connects through
// \Device\ConDrv\Connect, which the driver associates with the server that
// owns the console -- inheriting our handle is what makes that us.
func launchClient(server uintptr) (string, func()) {
	var si syscall.StartupInfo
	var pi syscall.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))
	argv, err := syscall.UTF16PtrFromString(`cmd.exe /c echo condrvprobe`)
	if err != nil {
		return "could not build a command line", nil
	}
	// CREATE_NEW_CONSOLE would give it a console of its own, which is the
	// opposite of what is wanted; the client must land on our endpoint.
	err = syscall.CreateProcess(nil, argv, nil, nil, true, 0, nil, nil, &si, &pi)
	if err != nil {
		return fmt.Sprintf("failed: %v", err), nil
	}
	return "cmd.exe started", func() {
		syscall.TerminateProcess(pi.Process, 0)
		syscall.CloseHandle(pi.Thread)
		syscall.CloseHandle(pi.Process)
	}
}

// createEvent makes the auto-reset event the driver signals.
func createEvent() (uintptr, error) {
	h, _, err := procCreateEventW.Call(0, 0, 0, 0)
	if h == 0 {
		return 0, err
	}
	return h, nil
}

// reportConhostAcceptance answers question 3: does the real conhost take a
// handle we made? If it does, our handle is the genuine article, and a proxy
// is a question of forwarding messages rather than of permission.
func reportConhostAcceptance(server uintptr) {
	say("--- Question 3: does the system conhost accept our handle? ---\n")
	say("ConPTY starts conhost as `conhost.exe --server <handle> --signal ...`.\n")
	say("If conhost accepts a handle we created, then f4 can hold the seat and\n")
	say("hand the work to the real conhost, watching the messages go past --\n")
	say("direction D2, with no C++ in the build and no console server of our own\n")
	say("to get right.\n")
	say("Note: this and question 2 cannot both succeed in one run. If conhost\n")
	say("serves the endpoint, conhost reads the messages, not us. A real proxy\n")
	say("therefore needs two endpoints -- ours facing the client, a second one\n")
	say("facing conhost -- and this question only asks whether the seat is\n")
	say("genuinely ours to hand over.\n")

	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	conhost := filepath.Join(sys, `System32\conhost.exe`)
	cmd := fmt.Sprintf(`"%s" --server 0x%X --headless --width 120 --height 30 -- cmd.exe /c echo condrvprobe`, conhost, server)

	var si syscall.StartupInfo
	var pi syscall.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))
	argv, _ := syscall.UTF16PtrFromString(cmd)
	err := syscall.CreateProcess(nil, argv, nil, nil, true,
		0x08000000 /* CREATE_NO_WINDOW */, nil, nil, &si, &pi)
	if err != nil {
		say("RESULT: CreateProcess failed: %v\n", err)
		say("VERDICT: inconclusive -- this is a launch problem, not an answer.\n\n")
		return
	}
	defer syscall.CloseHandle(pi.Thread)
	defer syscall.CloseHandle(pi.Process)

	ev, _ := syscall.WaitForSingleObject(pi.Process, 5000)
	if ev == uint32(syscall.WAIT_TIMEOUT) {
		say("RESULT: conhost is still running after 5s, holding our handle.\n")
		say("VERDICT: ACCEPTED. It did not reject the handle, which is the\n")
		say("         answer direction D2 needed. Terminating it.\n")
		syscall.TerminateProcess(pi.Process, 0)
		say("\n")
		return
	}
	var code uint32
	syscall.GetExitCodeProcess(pi.Process, &code)
	say("RESULT: conhost exited quickly with code %d (0x%X).\n", int32(code), code)
	if code == 0 {
		say("VERDICT: it ran and finished -- the handle was usable.\n")
	} else {
		say("VERDICT: it refused the handle. Record the code: this is what\n")
		say("         decides whether D2 is possible on this build.\n")
	}
	say("\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// fileVersion reads a file's version resource, so a report names the exact
// condrv.sys and conhost.exe its answers came from.
func fileVersion(path string) string {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "unknown"
	}
	size, _, _ := procGetFileVersionSize.Call(uintptr(unsafe.Pointer(p)), 0)
	if size == 0 {
		return "unknown"
	}
	buf := make([]byte, size)
	r, _, _ := procGetFileVersionInfo.Call(uintptr(unsafe.Pointer(p)), 0, size,
		uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return "unknown"
	}
	sub, _ := syscall.UTF16PtrFromString(`\`)
	var ptr uintptr
	var length uint32
	r, _, _ = procVerQueryValue.Call(uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(sub)), uintptr(unsafe.Pointer(&ptr)),
		uintptr(unsafe.Pointer(&length)))
	if r == 0 || ptr == 0 {
		return "unknown"
	}
	// Read the fixed info through the slice that owns the memory rather than
	// through the raw address: converting a uintptr back to a pointer is the
	// one unsafe pattern the garbage collector may break.
	off := int(ptr - uintptr(unsafe.Pointer(&buf[0])))
	if off < 0 || off+16 > len(buf) {
		return "unknown"
	}
	le := func(at int) uint32 {
		return uint32(buf[off+at]) | uint32(buf[off+at+1])<<8 |
			uint32(buf[off+at+2])<<16 | uint32(buf[off+at+3])<<24
	}
	if le(0) != 0xfeef04bd {
		return "unknown"
	}
	ms, ls := le(8), le(12)
	return fmt.Sprintf("%d.%d.%d.%d", ms>>16, ms&0xffff, ls>>16, ls&0xffff)
}

var _ = procCreateFileW
var _ = ioctlGetSrvPID
var _ = ioctlSetSrvInfo
