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
	ntdll                                 = syscall.NewLazyDLL("ntdll.dll")
	kernel32                              = syscall.NewLazyDLL("kernel32.dll")
	version                               = syscall.NewLazyDLL("version.dll")
	procNtCreateFile                      = ntdll.NewProc("NtCreateFile")
	procRtlInitUnicodeStr                 = ntdll.NewProc("RtlInitUnicodeString")
	procRtlGetVersion                     = ntdll.NewProc("RtlGetVersion")
	procDeviceIoControl                   = kernel32.NewProc("DeviceIoControl")
	procCreateFileW                       = kernel32.NewProc("CreateFileW")
	procGetFileVersionInfo                = version.NewProc("GetFileVersionInfoW")
	procVerQueryValue                     = version.NewProc("VerQueryValueW")
	procGetFileVersionSize                = version.NewProc("GetFileVersionInfoSizeW")
	procCreateEventW                      = kernel32.NewProc("CreateEventW")
	procWaitForSingleObject               = kernel32.NewProc("WaitForSingleObject")
	procCreatePseudoConsole               = kernel32.NewProc("CreatePseudoConsole")
	procClosePseudoConsole                = kernel32.NewProc("ClosePseudoConsole")
	procFreeConsole                       = kernel32.NewProc("FreeConsole")
	procAllocConsole                      = kernel32.NewProc("AllocConsole")
	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
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
		reportMessages(h)
		syscall.CloseHandle(syscall.Handle(h))
	}
	// Question 3 needs an endpoint nobody has claimed. The previous run
	// answered "refused" only because the probe had already made itself the
	// server on that same handle -- which is itself the finding that an
	// endpoint has exactly one server. So it gets a fresh one.
	if h2 := reportServerEndpointQuiet(); h2 != 0 {
		defer syscall.CloseHandle(syscall.Handle(h2))
		reportConhostAcceptance(h2)
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

// reportServerEndpointQuiet makes a second, unclaimed endpoint for question
// 3, without repeating question 1's explanation.
func reportServerEndpointQuiet() uintptr {
	name := `\Device\ConDrv\Server`
	var us unicodeString
	p, _ := syscall.UTF16PtrFromString(name)
	procRtlInitUnicodeStr.Call(uintptr(unsafe.Pointer(&us)), uintptr(unsafe.Pointer(p)))
	oa := objectAttributes{ObjectName: &us, Attributes: 0x00000002}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var handle uintptr
	var iosb ioStatusBlock
	st, _, _ := procNtCreateFile.Call(
		uintptr(unsafe.Pointer(&handle)), 0x10000000|0x00100000,
		uintptr(unsafe.Pointer(&oa)), uintptr(unsafe.Pointer(&iosb)),
		0, 0, 3, 2, 0x20, 0, 0)
	if st != 0 {
		say("(could not create a second endpoint for question 3: 0x%08X)\n\n", uint32(st))
		return 0
	}
	return handle
}

// reportMessages answers question 2, and does it by trying **every** way a
// client can be put on our endpoint in one run, rather than one per trip to
// the machine. Each strategy reports what it did, whether the driver saw a
// client (GET_SERVER_PID), and whether a message arrived.
//
// Why several: a console client does not choose its console from its
// standard handles. It attaches during startup in kernelbase, using the
// console handle inherited in its process parameters, which an ordinary
// CreateProcess fills in with *the parent's* console. Handing it our
// \Input and \Output is therefore not enough -- the previous run proved
// that, with all three child objects opening cleanly and no client
// attaching. The ways to actually redirect it are: the documented
// pseudoconsole attribute; inheriting our console by having the probe
// itself attach to the endpoint first; and asking the driver to create the
// process, which is how Windows starts conhost.
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
		say("VERDICT: we hold the endpoint but may not act as its server.\n\n")
		return
	}
	say("SET_SERVER_INFORMATION: accepted -- we are this endpoint's server.\n")

	ref, in, out2, ok := openChildObjects(server)
	if !ok {
		say("VERDICT: the endpoint has no usable child objects; nothing can attach.\n\n")
		return
	}
	defer closeAll(ref, in, out2)

	for _, strat := range []struct {
		name string
		run  func(server, in, out2 uintptr) (string, func())
	}{
		{"A: standard handles only (what the last run did)", attachByStdHandles},
		{"B: PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE", attachByPseudoConsoleAttribute},
		{"C: probe attaches first, child inherits the console", attachByInheritingOurConsole},
	} {
		say("\nstrategy %s\n", strat.name)
		what, cleanup := strat.run(server, in, out2)
		say("  launch: %s\n", what)
		if cleanup != nil {
			// Give the client a moment to start and call something.
			attached, pid := clientAttached(server)
			if attached {
				say("  GET_SERVER_PID: %d -- ATTACHED to our endpoint\n", pid)
			} else {
				say("  GET_SERVER_PID: nothing attached here\n")
			}
			readOneMessage(server, ev)
			cleanup()
		}
	}
	say("\n")
	reportReferenceSession()
}

// reportReferenceSession records what the *working* path produces, so that
// whichever strategy above lands, the next step has something to compare
// against without another trip to the machine: a real ConPTY session, its
// first bytes, and the console geometry cmd.exe sees inside it.
func reportReferenceSession() {
	say("--- Reference: what a normal ConPTY session looks like ---\n")
	say("Not a question, a baseline. If f4 ever reads console API messages, the\n")
	say("text in them must reconstruct exactly this.\n")

	var rIn, wIn, rOut, wOut syscall.Handle
	if err := syscall.CreatePipe(&rIn, &wIn, nil, 0); err != nil {
		say("CreatePipe failed: %v\n\n", err)
		return
	}
	if err := syscall.CreatePipe(&rOut, &wOut, nil, 0); err != nil {
		say("CreatePipe failed: %v\n\n", err)
		return
	}
	var hpc uintptr
	size := uint32(30)<<16 | uint32(120)
	hr, _, _ := procCreatePseudoConsole.Call(uintptr(size), uintptr(rIn), uintptr(wOut), 0,
		uintptr(unsafe.Pointer(&hpc)))
	if hr != 0 {
		say("CreatePseudoConsole failed: 0x%08X\n\n", uint32(hr))
		return
	}
	defer procClosePseudoConsole.Call(hpc)

	var listSize uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&listSize)))
	attrs := make([]byte, listSize)
	procInitializeProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&attrs[0])), 1, 0,
		uintptr(unsafe.Pointer(&listSize)))
	procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(&attrs[0])), 0,
		0x00020016, hpc, unsafe.Sizeof(hpc), 0, 0)

	type startupInfoEx struct {
		StartupInfo syscall.StartupInfo
		AttrList    uintptr
	}
	var six startupInfoEx
	six.StartupInfo.Cb = uint32(unsafe.Sizeof(six))
	six.AttrList = uintptr(unsafe.Pointer(&attrs[0]))
	var pi syscall.ProcessInformation
	// A command whose output says what the console inside the pty believes
	// its width to be: the number every wrap decision is made against.
	argv, _ := syscall.UTF16PtrFromString(`cmd.exe /c mode con & echo condrvprobe-ref`)
	if err := syscall.CreateProcess(nil, argv, nil, nil, false, 0x00080000, nil, nil,
		&six.StartupInfo, &pi); err != nil {
		say("CreateProcess failed: %v\n\n", err)
		return
	}
	defer killer(pi)()
	syscall.CloseHandle(wOut)

	buf := make([]byte, 8192)
	total := 0
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && total < len(buf) {
		var n uint32
		if err := syscall.ReadFile(rOut, buf[total:], &n, nil); err != nil || n == 0 {
			break
		}
		total += int(n)
	}
	say("read %d bytes from the pty\n", total)
	if total > 0 {
		show := total
		if show > 400 {
			show = 400
		}
		say("first %d bytes, quoted: %q\n", show, string(buf[:show]))
	}
	closeAll(uintptr(rIn), uintptr(wIn), uintptr(rOut))
	say("\n")
}

// openChildObjects creates the console objects that live under a server
// endpoint. All three opened cleanly on 10.0.22000, so the endpoint is a
// real console; what remains is making a client choose it.
func openChildObjects(server uintptr) (ref, in, out uintptr, ok bool) {
	open := func(name string) (uintptr, uintptr) {
		var us unicodeString
		p, _ := syscall.UTF16PtrFromString(name)
		procRtlInitUnicodeStr.Call(uintptr(unsafe.Pointer(&us)), uintptr(unsafe.Pointer(p)))
		oa := objectAttributes{RootDirectory: server, ObjectName: &us, Attributes: 0x00000002}
		oa.Length = uint32(unsafe.Sizeof(oa))
		var h uintptr
		var iosb ioStatusBlock
		st, _, _ := procNtCreateFile.Call(
			uintptr(unsafe.Pointer(&h)), 0x80000000|0x40000000|0x00100000,
			uintptr(unsafe.Pointer(&oa)), uintptr(unsafe.Pointer(&iosb)),
			0, 0, 3, 2 /* FILE_CREATE */, 0x20, 0, 0)
		return h, st
	}
	var st uintptr
	ref, st = open(`\Reference`)
	say("  open \\Reference: NTSTATUS 0x%08X\n", uint32(st))
	in, st = open(`\Input`)
	say("  open \\Input:     NTSTATUS 0x%08X\n", uint32(st))
	out, st = open(`\Output`)
	say("  open \\Output:    NTSTATUS 0x%08X\n", uint32(st))
	return ref, in, out, ref != 0 && in != 0 && out != 0
}

func closeAll(hs ...uintptr) {
	for _, h := range hs {
		if h != 0 {
			syscall.CloseHandle(syscall.Handle(h))
		}
	}
}

// attachByStdHandles is the strategy that already failed, kept so one report
// shows it failing beside the others rather than by memory.
func attachByStdHandles(server, in, out uintptr) (string, func()) {
	var si syscall.StartupInfo
	var pi syscall.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = syscall.STARTF_USESTDHANDLES
	si.StdInput, si.StdOutput, si.StdErr = syscall.Handle(in), syscall.Handle(out), syscall.Handle(out)
	argv, _ := syscall.UTF16PtrFromString(`cmd.exe /c echo condrvprobe-A`)
	if err := syscall.CreateProcess(nil, argv, nil, nil, true, 0, nil, nil, &si, &pi); err != nil {
		return fmt.Sprintf("CreateProcess failed: %v", err), nil
	}
	return "started", killer(pi)
}

// attachByPseudoConsoleAttribute uses the documented ConPTY attribute, which
// is precisely "this is your console". It needs a pseudoconsole, so the probe
// makes one with CreatePseudoConsole -- note this is *not* our endpoint, so a
// success here proves the mechanism works, not that it can be pointed at an
// endpoint we own. That is the next question if this is the one that lands.
func attachByPseudoConsoleAttribute(server, in, out uintptr) (string, func()) {
	var hpc uintptr
	var rIn, wIn, rOut, wOut syscall.Handle
	if err := syscall.CreatePipe(&rIn, &wIn, nil, 0); err != nil {
		return fmt.Sprintf("CreatePipe failed: %v", err), nil
	}
	if err := syscall.CreatePipe(&rOut, &wOut, nil, 0); err != nil {
		return fmt.Sprintf("CreatePipe failed: %v", err), nil
	}
	size := uint32(30)<<16 | uint32(120)
	hr, _, _ := procCreatePseudoConsole.Call(uintptr(size), uintptr(rIn), uintptr(wOut), 0,
		uintptr(unsafe.Pointer(&hpc)))
	if hr != 0 {
		return fmt.Sprintf("CreatePseudoConsole failed: 0x%08X", uint32(hr)), nil
	}

	var size2 uintptr
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size2)))
	attrs := make([]byte, size2)
	procInitializeProcThreadAttributeList.Call(uintptr(unsafe.Pointer(&attrs[0])), 1, 0,
		uintptr(unsafe.Pointer(&size2)))
	const procThreadAttributePseudoConsole = 0x00020016
	procUpdateProcThreadAttribute.Call(uintptr(unsafe.Pointer(&attrs[0])), 0,
		procThreadAttributePseudoConsole, hpc, unsafe.Sizeof(hpc), 0, 0)

	type startupInfoEx struct {
		StartupInfo syscall.StartupInfo
		AttrList    uintptr
	}
	var six startupInfoEx
	six.StartupInfo.Cb = uint32(unsafe.Sizeof(six))
	six.AttrList = uintptr(unsafe.Pointer(&attrs[0]))
	var pi syscall.ProcessInformation
	argv, _ := syscall.UTF16PtrFromString(`cmd.exe /c echo condrvprobe-B`)
	const extendedStartupInfoPresent = 0x00080000
	err := syscall.CreateProcess(nil, argv, nil, nil, false,
		extendedStartupInfoPresent, nil, nil, &six.StartupInfo, &pi)
	if err != nil {
		return fmt.Sprintf("CreateProcess failed: %v", err), nil
	}
	return "started under a pseudoconsole (mechanism check)", func() {
		killer(pi)()
		procClosePseudoConsole.Call(hpc)
		closeAll(uintptr(rIn), uintptr(wIn), uintptr(rOut), uintptr(wOut))
	}
}

// attachByInheritingOurConsole makes the probe itself a client of the
// endpoint first, so an ordinary CreateProcess passes *that* console down.
// If this works it is the cheapest route by far -- and it is how a terminal
// would do it anyway, since f4 wants the console for itself.
func attachByInheritingOurConsole(server, in, out uintptr) (string, func()) {
	// Detach from whatever console we have, then attach to the endpoint's.
	procFreeConsole.Call()
	// The client side of an endpoint is \Connect; opening it is what
	// AllocConsole does under the covers.
	var us unicodeString
	p, _ := syscall.UTF16PtrFromString(`\Connect`)
	procRtlInitUnicodeStr.Call(uintptr(unsafe.Pointer(&us)), uintptr(unsafe.Pointer(p)))
	oa := objectAttributes{RootDirectory: server, ObjectName: &us, Attributes: 0x00000002}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var conn uintptr
	var iosb ioStatusBlock
	st, _, _ := procNtCreateFile.Call(
		uintptr(unsafe.Pointer(&conn)), 0x80000000|0x40000000|0x00100000,
		uintptr(unsafe.Pointer(&oa)), uintptr(unsafe.Pointer(&iosb)),
		0, 0, 3, 2, 0x20, 0, 0)
	say("  open \\Connect: NTSTATUS 0x%08X\n", uint32(st))

	var si syscall.StartupInfo
	var pi syscall.ProcessInformation
	si.Cb = uint32(unsafe.Sizeof(si))
	argv, _ := syscall.UTF16PtrFromString(`cmd.exe /c echo condrvprobe-C`)
	err := syscall.CreateProcess(nil, argv, nil, nil, true, 0, nil, nil, &si, &pi)
	if err != nil {
		return fmt.Sprintf("CreateProcess failed: %v", err), nil
	}
	return "started after the probe attached to the endpoint", func() {
		killer(pi)()
		closeAll(conn)
		// Put a console back so the report can still be printed.
		procAllocConsole.Call()
	}
}

func killer(pi syscall.ProcessInformation) func() {
	return func() {
		syscall.TerminateProcess(pi.Process, 0)
		syscall.CloseHandle(pi.Thread)
		syscall.CloseHandle(pi.Process)
	}
}

// clientAttached asks the driver whether anything is on this endpoint. It
// retries briefly: a process takes a moment to reach its console attach.
func clientAttached(server uintptr) (bool, uint32) {
	var pid, returned uint32
	for i := 0; i < 20; i++ {
		r, _, _ := procDeviceIoControl.Call(server, uintptr(ioctlGetSrvPID),
			0, 0, uintptr(unsafe.Pointer(&pid)), unsafe.Sizeof(pid),
			uintptr(unsafe.Pointer(&returned)), 0)
		if r != 0 && pid != 0 {
			return true, pid
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false, 0
}

// readOneMessage waits briefly for a message and records its bytes. The
// layout is undocumented (microsoft/terminal#10463), so the bytes are
// recorded rather than interpreted: only the same shape on another build is
// evidence that it is stable.
func readOneMessage(server, ev uintptr) {
	w, _, _ := procWaitForSingleObject.Call(ev, 1500)
	if w != 0 {
		say("  message: none within 1.5s (wait 0x%X)\n", w)
		return
	}
	buf := make([]byte, 4096)
	var returned uint32
	r, _, err := procDeviceIoControl.Call(server, uintptr(ioctlReadIO),
		0, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
		uintptr(unsafe.Pointer(&returned)), 0)
	if r == 0 {
		say("  message: the event fired but READ_IO failed: %v\n", err)
		return
	}
	n := int(returned)
	say("  MESSAGE ARRIVED, %d bytes\n", n)
	if n > 96 {
		n = 96
	}
	say("  first %d bytes: % x\n", n, buf[:n])
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
	say("Note: this runs on a FRESH endpoint, not the one above. The first run\n")
	say("of this probe saw conhost accept our handle; the second saw it refuse\n")
	say("with 0x80070016, and the only difference was that the probe had claimed\n")
	say("the server role in between. An endpoint has exactly one server, so a\n")
	say("proxy needs two: ours facing the client, a second facing conhost.\n")

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
