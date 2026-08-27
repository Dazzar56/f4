//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type procInfo struct {
	PID    uint32
	PPID   uint32
	Name   string
	Path   string
	Subsys string // "GUI", "CUI", "?"
}

func snapshotProcesses() []procInfo {
	h, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)
	var e syscall.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []procInfo
	if err := syscall.Process32First(h, &e); err != nil {
		return nil
	}
	for {
		out = append(out, procInfo{
			PID:  e.ProcessID,
			PPID: e.ParentProcessID,
			Name: syscall.UTF16ToString(e.ExeFile[:]),
		})
		if err := syscall.Process32Next(h, &e); err != nil {
			break
		}
	}
	return out
}

// childrenOf returns the direct children of pid, with image path and PE
// subsystem filled in. The subsystem is what tells notepad (GUI) from a
// nested cmd (CUI) without running anything -- see TERMINAL_LEDGER C5.
func childrenOf(pid uint32, all []procInfo) []procInfo {
	var out []procInfo
	for _, p := range all {
		if p.PPID != pid {
			continue
		}
		p.Path = processImagePath(p.PID)
		p.Subsys = peSubsystem(p.Path)
		out = append(out, p)
	}
	return out
}

// waitForDirectChildren observes the child rather than inferring its lifetime
// from a quiet VT stream. Some console programs (notably timeout.exe) remain
// silent while they are still reading the pseudoconsole input.
func waitForDirectChildren(pid uint32, timeout time.Duration) ([]procInfo, time.Duration) {
	start := time.Now()
	deadline := start.Add(timeout)
	for {
		if kids := childrenOf(pid, snapshotProcesses()); len(kids) != 0 {
			return kids, time.Since(start)
		}
		if time.Now().After(deadline) {
			return nil, time.Since(start)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// waitForProcessesGone waits for the exact PIDs that were observed. Looking
// only for "no current children" after the fact can miss a child that has not
// appeared yet, which is the race this probe is meant to rule out.
func waitForProcessesGone(procs []procInfo, timeout time.Duration) ([]procInfo, time.Duration) {
	wanted := make(map[uint32]bool, len(procs))
	for _, p := range procs {
		wanted[p.PID] = true
	}
	start := time.Now()
	deadline := start.Add(timeout)
	for {
		var alive []procInfo
		for _, p := range snapshotProcesses() {
			if wanted[p.PID] {
				p.Path = processImagePath(p.PID)
				p.Subsys = peSubsystem(p.Path)
				alive = append(alive, p)
			}
		}
		if len(alive) == 0 {
			return nil, time.Since(start)
		}
		if time.Now().After(deadline) {
			return alive, time.Since(start)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func descendantsOf(pid uint32, all []procInfo, depth int) []procInfo {
	if depth <= 0 {
		return nil
	}
	kids := childrenOf(pid, all)
	out := append([]procInfo(nil), kids...)
	for _, k := range kids {
		out = append(out, descendantsOf(k.PID, all, depth-1)...)
	}
	return out
}

func processName(pid uint32, all []procInfo) string {
	for _, p := range all {
		if p.PID == pid {
			return p.Name
		}
	}
	return "?"
}

func parentOf(pid uint32, all []procInfo) uint32 {
	for _, p := range all {
		if p.PID == pid {
			return p.PPID
		}
	}
	return 0
}

func processesNamed(all []procInfo, name string) []procInfo {
	var out []procInfo
	for _, p := range all {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		p.Path = processImagePath(p.PID)
		p.Subsys = peSubsystem(p.Path)
		out = append(out, p)
	}
	return out
}

func newProcessesNamed(before, after []procInfo, name string) []procInfo {
	old := make(map[uint32]bool)
	for _, p := range processesNamed(before, name) {
		old[p.PID] = true
	}
	var out []procInfo
	for _, p := range processesNamed(after, name) {
		if !old[p.PID] {
			out = append(out, p)
		}
	}
	return out
}

func waitForNewProcessesNamed(before []procInfo, name string, timeout time.Duration) ([]procInfo, []procInfo, time.Duration) {
	start := time.Now()
	deadline := start.Add(timeout)
	var firstSeen time.Time
	for {
		after := snapshotProcesses()
		found := newProcessesNamed(before, after, name)
		if len(found) != 0 {
			if firstSeen.IsZero() {
				firstSeen = time.Now()
			} else if time.Since(firstSeen) >= 750*time.Millisecond {
				return after, found, time.Since(start)
			}
		} else {
			firstSeen = time.Time{}
		}
		if time.Now().After(deadline) {
			return after, found, time.Since(start)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func describeProcessChain(pid uint32, all []procInfo) string {
	var chain []string
	for i := 0; i < 8 && pid != 0; i++ {
		chain = append(chain, fmt.Sprintf("%s(%d)", processName(pid, all), pid))
		pid = parentOf(pid, all)
	}
	return strings.Join(chain, " <- ")
}

// peSubsystem reads the Subsystem field of the PE optional header. It sits at
// offset 68 of the optional header for both PE32 and PE32+.
func peSubsystem(path string) string {
	if path == "" {
		return "?"
	}
	f, err := os.Open(path)
	if err != nil {
		return "?"
	}
	defer f.Close()
	hdr := make([]byte, 0x400)
	n, _ := f.Read(hdr)
	hdr = hdr[:n]
	if len(hdr) < 0x40 || hdr[0] != 'M' || hdr[1] != 'Z' {
		return "?"
	}
	off := int(binary.LittleEndian.Uint32(hdr[0x3c:]))
	if off+24+70 > len(hdr) {
		return "?"
	}
	if string(hdr[off:off+4]) != "PE\x00\x00" {
		return "?"
	}
	sub := binary.LittleEndian.Uint16(hdr[off+24+68:])
	switch sub {
	case 2:
		return "GUI"
	case 3:
		return "CUI"
	default:
		return fmt.Sprintf("subsys%d", sub)
	}
}

func describeProcs(ps []procInfo) string {
	if len(ps) == 0 {
		return "(none)"
	}
	s := ""
	for i, p := range ps {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s[pid=%d,%s]", p.Name, p.PID, p.Subsys)
	}
	return s
}

// windowsOfPID lists top-level windows belonging to a pid: class, title,
// visibility. Used to check P3 (a console title is not readable from outside
// a pseudoconsole) and F2 (the ConPTY pseudo window).
func windowsOfPID(pid uint32) []string {
	var out []string
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		if windowPID(hwnd) == pid {
			out = append(out, fmt.Sprintf("hwnd=%#x class=%q title=%q visible=%v style=%#x exstyle=%#x",
				hwnd, className(hwnd), windowText(hwnd), isWindowVisible(hwnd),
				windowLong(hwnd, gwlStyle), windowLong(hwnd, gwlExStyle)))
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}
