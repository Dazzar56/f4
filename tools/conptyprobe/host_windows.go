//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// queryTerminal writes a VT query to the console and reads the answer back out
// of the console input buffer. It works whether or not ENABLE_VIRTUAL_TERMINAL_INPUT
// is on: conhost delivers the answer as key events either way.
//
// This is how WINCON_805_HANDOVER Q4 gets closed -- "what does this build's
// conhost answer to DA1" -- without asking anyone to install anything.
func queryTerminal(query string, final byte, timeout time.Duration) (string, error) {
	hIn := getStdHandle(stdInputHandle)
	hOut := getStdHandle(stdOutputHandle)
	inMode, okIn := getConsoleMode(hIn)
	outMode, okOut := getConsoleMode(hOut)
	if !okIn || !okOut {
		return "", fmt.Errorf("not attached to a console (input or output redirected)")
	}
	setConsoleMode(hOut, outMode|enableVTProcessing)
	setConsoleMode(hIn, (inMode&^(enableLineInput|enableEchoInput|enableProcessedInput))|enableWindowInput)
	defer func() {
		setConsoleMode(hIn, inMode)
		setConsoleMode(hOut, outMode)
	}()

	procFlushConsoleInputBuffer.Call(uintptr(hIn))
	os.Stdout.WriteString(query)

	deadline := time.Now().Add(timeout)
	var sb strings.Builder
	for time.Now().Before(deadline) {
		left := uint32(time.Until(deadline) / time.Millisecond)
		ev, err := syscall.WaitForSingleObject(hIn, left)
		if err != nil || ev != 0 {
			break
		}
		var rec inputRecord
		var read uint32
		r, _, _ := procReadConsoleInputW.Call(uintptr(hIn), uintptr(unsafe.Pointer(&rec)), 1,
			uintptr(unsafe.Pointer(&read)))
		if r == 0 || read == 0 {
			break
		}
		if rec.EventType != 1 || rec.KeyDown == 0 || rec.UnicodeChar == 0 {
			continue
		}
		sb.WriteRune(rune(rec.UnicodeChar))
		s := sb.String()
		if len(s) > 0 && s[len(s)-1] == final && len(s) > 1 {
			return s, nil
		}
		if strings.HasSuffix(s, "\x1b\\") {
			return s, nil
		}
		if sb.Len() > 512 {
			return s, nil
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("no answer within %v", timeout)
	}
	return sb.String(), nil
}

func describeHost(w *writer) {
	w.section("1. The terminal this probe is running in")

	v := rtlGetVersion()
	build := fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
	const currentVersionKey = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	ubr := regString(currentVersionKey, "UBR")
	if ubr != "" {
		build += "." + ubr
	}
	product := regString(currentVersionKey, "ProductName")
	display := regString(currentVersionKey, "DisplayVersion")
	releaseID := regString(currentVersionKey, "ReleaseId")
	edition := regString(currentVersionKey, "EditionID")
	installation := regString(currentVersionKey, "InstallationType")
	buildLab := regString(currentVersionKey, "BuildLabEx")
	w.printf("windows      = %s  %s %s\n", build, product, display)
	w.printf("release      = DisplayVersion=%q ReleaseId=%q EditionID=%q InstallationType=%q\n",
		display, releaseID, edition, installation)
	w.printf("build lab    = %q\n", buildLab)
	w.summary("build", build)
	w.summary("product", strings.TrimSpace(product+" "+display))
	w.summary("os.family", windowsFamily(v.BuildNumber))
	w.summary("os.display_version", emptyAs(display, "(unset)"))
	w.summary("os.release_id", emptyAs(releaseID, "(unset)"))
	w.summary("os.edition", emptyAs(edition, "(unset)"))
	w.summary("os.installation_type", emptyAs(installation, "(unset)"))
	w.summary("os.build_lab", emptyAs(buildLab, "(unset)"))

	for _, name := range []string{
		"WT_SESSION", "WT_PROFILE_ID", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
		"COLORTERM", "ConEmuANSI", "ConEmuPID", "SESSIONNAME", "VTUI_GRAPHICS",
		"VTUI_DEBUG", "F4_WIN_REFLOW", "PROMPT",
	} {
		val, ok := os.LookupEnv(name)
		if !ok {
			val = "(unset)"
		}
		w.printf("env %-20s = %s\n", name, val)
	}
	w.summary("env.WT_SESSION", envOr("WT_SESSION", "(unset)"))

	// Who started us: the chain says whether this is conhost, Windows Terminal
	// with a handoff, or something else entirely.
	all := snapshotProcesses()
	pid := uint32(os.Getpid())
	chain := []string{}
	for i := 0; i < 6 && pid != 0; i++ {
		chain = append(chain, fmt.Sprintf("%s(%d)", processName(pid, all), pid))
		pid = parentOf(pid, all)
	}
	w.printf("process chain= %s\n", strings.Join(chain, " <- "))
	w.summary("host.chain", strings.Join(chain, "<-"))

	// Sibling terminal hosts. GetConsoleWindow lies under Windows Terminal
	// (WINCON_805_HANDOVER F2), so name the processes as well.
	var hosts []string
	for _, p := range all {
		switch strings.ToLower(p.Name) {
		case "conhost.exe", "openconsole.exe", "windowsterminal.exe":
			path := processImagePath(p.PID)
			hosts = append(hosts, fmt.Sprintf("%s(%d,parent=%d,path=%q,version=%s)",
				p.Name, p.PID, p.PPID, path, fileVersion(path)))
		}
	}
	w.printf("console hosts= %s\n", strings.Join(hosts, " "))
	w.summary("host.processes", emptyAs(strings.Join(hosts, " "), "(none)"))

	// GetConsoleProcessList: which processes are attached to *our* console.
	var listBuf [64]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&listBuf[0])), uintptr(len(listBuf)))
	if n > 0 && int(n) <= len(listBuf) {
		var names []string
		for _, cpid := range listBuf[:n] {
			names = append(names, fmt.Sprintf("%s(%d)", processName(cpid, all), cpid))
		}
		w.printf("attached     = %s\n", strings.Join(names, " "))
	} else {
		w.printf("attached     = GetConsoleProcessList returned %d\n", n)
	}

	hwnd := consoleWindow()
	hostClass := ""
	hostVisible := false
	ownerClass := ""
	if hwnd == 0 {
		w.printf("GetConsoleWindow = 0 (no console window)\n")
		w.summary("host.window.class", "(none)")
	} else {
		cls := className(hwnd)
		hostClass = cls
		hostVisible = isWindowVisible(hwnd)
		st := windowLong(hwnd, gwlStyle)
		ex := windowLong(hwnd, gwlExStyle)
		cr := clientRect(hwnd)
		wr := windowRect(hwnd)
		owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
		ownerClass = className(owner)
		par := windowLong(hwnd, gwlHwndPar)
		w.printf("GetConsoleWindow = %#x class=%q title=%q\n", hwnd, cls, windowText(hwnd))
		w.printf("  visible=%v iconic=%v client=%dx%d window=%dx%d at %d,%d\n",
			isWindowVisible(hwnd), isIconic(hwnd), cr.Right-cr.Left, cr.Bottom-cr.Top,
			wr.Right-wr.Left, wr.Bottom-wr.Top, wr.Left, wr.Top)
		w.printf("  style=%#x exstyle=%#x WS_CLIPCHILDREN=%s WS_EX_LAYERED=%s WS_EX_TOOLWINDOW=%s\n",
			st, ex, yesno(st&wsClipChildren != 0), yesno(ex&wsExLayered != 0), yesno(ex&wsExToolWindow != 0))
		w.printf("  owner=%#x (class %q) GWLP_HWNDPARENT=%#x pid=%d (%s)\n",
			owner, ownerClass, par, windowPID(hwnd), processName(windowPID(hwnd), all))
		if dpi, _, _ := procGetDpiForWindow.Call(hwnd); dpi != 0 {
			w.printf("  dpi=%d\n", dpi)
		}
		w.summary("host.window.class", cls)
		w.summary("host.window.visible", yesno(isWindowVisible(hwnd)))
		w.summary("host.window.client", fmt.Sprintf("%dx%d", cr.Right-cr.Left, cr.Bottom-cr.Top))
		w.summary("host.window.clipchildren", yesno(st&wsClipChildren != 0))
		w.summary("host.window.owner_class", emptyAs(ownerClass, "(none)"))
		// This is exactly the decision WINCON_805_HANDOVER step 1 asks f4 to make.
		trust := "no"
		if cls == "ConsoleWindowClass" && isWindowVisible(hwnd) && cr.Right-cr.Left > 0 {
			trust = "yes"
		}
		w.summary("host.window.overlayable", trust)
	}
	identity := classifyHostIdentity(hostClass, ownerClass, hostVisible, os.Getenv("WT_SESSION"),
		strings.Join(chain, " ")+" "+strings.Join(hosts, " "))
	w.printf("host kind     = %s\n", identity.Kind)
	w.printf("launch mode   = %s\n", identity.LaunchMode)
	w.summary("host.kind", identity.Kind)
	w.summary("host.launch_mode", identity.LaunchMode)

	const startupKey = `Console\%%Startup`
	delegationConsole := regStringAt(hkeyCurrentUser, startupKey, "DelegationConsole")
	delegationTerminal := regStringAt(hkeyCurrentUser, startupKey, "DelegationTerminal")
	w.printf("default terminal delegation: console=%q terminal=%q\n", delegationConsole, delegationTerminal)
	w.summary("host.delegation.console", emptyAs(delegationConsole, "(unset)"))
	w.summary("host.delegation.terminal", emptyAs(delegationTerminal, "(unset)"))

	hOut := getStdHandle(stdOutputHandle)
	hIn := getStdHandle(stdInputHandle)
	if m, ok := getConsoleMode(hOut); ok {
		w.printf("console out mode = %#x (VT_PROCESSING=%s)\n", m, yesno(m&enableVTProcessing != 0))
	}
	if m, ok := getConsoleMode(hIn); ok {
		w.printf("console in  mode = %#x (VT_INPUT=%s)\n", m, yesno(m&enableVTInput != 0))
	}
	cp, _, _ := procGetConsoleCP.Call()
	ocp, _, _ := procGetConsoleOutputCP.Call()
	w.printf("codepage in/out  = %d/%d\n", cp, ocp)
	var sbi consoleScreenBufferInfo
	if r, _, _ := procGetConsoleScreenBufferInfo.Call(uintptr(hOut), uintptr(unsafe.Pointer(&sbi))); r != 0 {
		w.printf("screen buffer    = %dx%d, viewport %dx%d\n", sbi.Size.X, sbi.Size.Y,
			sbi.Window.Right-sbi.Window.Left+1, sbi.Window.Bottom-sbi.Window.Top+1)
	}
	title := make([]uint16, 512)
	if n, _, _ := procGetConsoleTitleW.Call(uintptr(unsafe.Pointer(&title[0])), uintptr(len(title))); n > 0 {
		w.printf("console title    = %q\n", syscall.UTF16ToString(title[:n]))
	}

	w.printf("\n-- VT queries to the terminal (no answer is itself an answer) --\n")
	type q struct {
		name, key, seq string
		final          byte
	}
	for _, item := range []q{
		{"DA1  (ESC[c)", "host.da1", "\x1b[c", 'c'},
		{"DA2  (ESC[>c)", "host.da2", "\x1b[>c", 'c'},
		{"XTVERSION (ESC[>q)", "host.xtversion", "\x1b[>q", '\\'},
		{"sixel color registers (ESC[?1;1;0S)", "host.sixel_color_registers", "\x1b[?1;1;0S", 'S'},
		{"sixel max geometry (ESC[?2;1;0S)", "host.sixel_max_geometry", "\x1b[?2;1;0S", 'S'},
	} {
		ans, err := queryTerminal(item.seq, item.final, 600*time.Millisecond)
		if err != nil {
			w.printf("%-36s -> %v\n", item.name, err)
			w.summary(item.key, err.Error())
			if item.key == "host.da1" {
				w.summary("host.da1.sixel", "unknown (no DA1 answer)")
			}
			continue
		}
		escaped := Escape([]byte(ans))
		w.printf("%-36s -> %s\n", item.name, escaped)
		w.summary(item.key, escaped)
		if item.key == "host.da1" {
			w.summary("host.da1.sixel", yesno(da1HasSixel(ans)))
		}
	}
}

func currentHostKind() string {
	return currentHostIdentity().Kind
}

func currentHostIdentity() hostIdentity {
	hwnd := consoleWindow()
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	all := snapshotProcesses()
	var context []string
	pid := uint32(os.Getpid())
	for i := 0; i < 8 && pid != 0; i++ {
		context = append(context, processName(pid, all))
		pid = parentOf(pid, all)
	}
	for _, p := range all {
		switch strings.ToLower(p.Name) {
		case "windowsterminal.exe", "openconsole.exe":
			context = append(context, p.Name)
		}
	}
	return classifyHostIdentity(className(hwnd), className(owner), hwnd != 0 && isWindowVisible(hwnd),
		os.Getenv("WT_SESSION"), strings.Join(context, " "))
}

func windowsFamily(build uint32) string {
	if build >= 22000 {
		return "Windows 11 family"
	}
	return "Windows 10 or older family"
}

func emptyAs(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// da1HasSixel looks for parameter 4 in a primary device attributes answer.
func da1HasSixel(ans string) bool {
	i := strings.Index(ans, "[")
	if i < 0 {
		return false
	}
	body := ans[i+1:]
	body = strings.TrimSuffix(strings.TrimSpace(body), "c")
	body = strings.TrimPrefix(body, "?")
	for _, p := range strings.Split(body, ";") {
		if strings.TrimSpace(p) == "4" {
			return true
		}
	}
	return false
}

func envOr(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}
	return def
}
