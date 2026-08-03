//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	gpuOnce   sync.Once
	cachedGPU []GPUInfo
)

// gpuInfo enumerates /sys/class/drm/card[N] and returns a best-effort
// human name for each. Order of preference:
//
//  1. /proc/driver/nvidia/gpus/*/information — NVIDIA proprietary
//     driver exposes the marketing name here in one line.
//  2. /sys/class/drm/cardN/device/label — sometimes present on
//     modern kernels (Chrome OS / ThinkPads set it; upstream doesn't
//     always).
//  3. PCI vendor/device IDs, resolved through /usr/share/hwdata/pci.ids
//     if the file exists — same DB lspci uses. No dependency added.
//  4. Raw PCI IDs (`8086:9a49`) as a last resort so the row is at
//     least identifiable.
//
// Everything is cached — the GPU set doesn't change at runtime.
func gpuInfo() ([]GPUInfo, bool) {
	gpuOnce.Do(func() {
		cachedGPU = enumerateLinuxGPUs()
	})
	return cachedGPU, len(cachedGPU) > 0
}

func enumerateLinuxGPUs() []GPUInfo {
	var out []GPUInfo
	// NVIDIA proprietary driver first — its names are cleaner than
	// anything you can build from PCI IDs alone.
	if names := readNvidiaGPUs(); len(names) > 0 {
		for _, n := range names {
			// The proprietary NVIDIA driver identifies itself as
			// "nvidia" — that's the module name loaded via
			// modprobe, same string sysfs reports for the DRM
			// card. Hard-coded rather than probed because
			// /proc/driver/nvidia only exists when that exact
			// module is loaded.
			out = append(out, GPUInfo{Model: n, Driver: "nvidia"})
		}
	}

	cards, _ := filepath.Glob("/sys/class/drm/card[0-9]*")
	// Filter out cardN-* connector entries (HDMI-A-1 etc.); only
	// keep the base card devices.
	filtered := cards[:0]
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") {
			continue
		}
		filtered = append(filtered, c)
	}
	for _, card := range filtered {
		dev := filepath.Join(card, "device")

		// Skip a card we already covered via the nvidia branch.
		if len(out) > 0 {
			vendor := strings.TrimSpace(readFileTrim(filepath.Join(dev, "vendor")))
			if strings.EqualFold(vendor, "0x10de") {
				continue
			}
		}

		driver := readDriverFromUevent(filepath.Join(dev, "uevent"))
		if label := strings.TrimSpace(readFileTrim(filepath.Join(dev, "label"))); label != "" {
			out = append(out, GPUInfo{Model: label, Driver: driver})
			continue
		}
		vendor := strings.TrimPrefix(strings.TrimSpace(readFileTrim(filepath.Join(dev, "vendor"))), "0x")
		device := strings.TrimPrefix(strings.TrimSpace(readFileTrim(filepath.Join(dev, "device"))), "0x")
		if vendor == "" && device == "" {
			continue
		}
		if name := lookupPCIName(vendor, device); name != "" {
			out = append(out, GPUInfo{Model: name, Driver: driver})
			continue
		}
		out = append(out, GPUInfo{Model: fmt.Sprintf("%s:%s", vendor, device), Driver: driver})
	}
	// WSL2 fallback. Microsoft's GPU passthrough uses dxgkrnl via
	// /dev/dxg instead of exposing anything under /sys/class/drm.
	// If we found nothing above and /dev/dxg is present, at least
	// tell the user *something* is there rather than hide the
	// section — the host adapter name would need dxgkio ioctl
	// plumbing which is out of scope for a display panel.
	if len(out) == 0 {
		if _, err := os.Stat("/dev/dxg"); err == nil {
			out = append(out, GPUInfo{
				Model:  Msg("InfoPanel.GPUWSLVirt"),
				Driver: "dxgkrnl",
			})
		}
	}
	return out
}

// readDriverFromUevent picks the DRIVER=... line out of a device's
// uevent file. Values are the kernel module name loaded for the
// card — "i915" (Intel), "amdgpu" / "radeon" (AMD), "nouveau"
// (open NVIDIA), "vmwgfx" / "virtio_gpu" on VMs. Empty on
// containers without /sys/class/drm or when the file is unreadable.
func readDriverFromUevent(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "DRIVER=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "DRIVER="))
		}
	}
	return ""
}

// readNvidiaGPUs pulls the "Model" line from each
// /proc/driver/nvidia/gpus/*/information. Empty when the module
// isn't loaded (nouveau or no-NVIDIA systems).
func readNvidiaGPUs() []string {
	entries, err := filepath.Glob("/proc/driver/nvidia/gpus/*/information")
	if err != nil || len(entries) == 0 {
		return nil
	}
	var out []string
	for _, path := range entries {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if colon := strings.IndexByte(line, ':'); colon > 0 {
				if strings.TrimSpace(line[:colon]) == "Model" {
					out = append(out, strings.TrimSpace(line[colon+1:]))
					break
				}
			}
		}
		f.Close()
	}
	return out
}

func readFileTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// lookupPCIName looks up a vendor:device pair in /usr/share/hwdata/pci.ids
// (present on essentially every distro that ships lspci). Returns
// "Vendor Device" or "" if the file or the IDs aren't found.
func lookupPCIName(vendor, device string) string {
	if vendor == "" {
		return ""
	}
	vendor = strings.ToLower(vendor)
	device = strings.ToLower(device)
	for _, path := range []string{"/usr/share/hwdata/pci.ids", "/usr/share/misc/pci.ids"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		vName, dName := scanPCIIDs(f, vendor, device)
		f.Close()
		if dName != "" && vName != "" {
			return vName + " " + dName
		}
		if vName != "" {
			return vName + " " + device
		}
	}
	return ""
}

// scanPCIIDs parses the pci.ids tab-indented format:
//
//	VVVV  Vendor name
//	<TAB>DDDD  Device name
//	<TAB><TAB>SSSS ...
//
// We stop scanning after the vendor block is exhausted (indent drops
// back to 0 for a new vendor line).
func scanPCIIDs(f *os.File, vendor, device string) (string, string) {
	var vName, dName string
	sc := bufio.NewScanner(f)
	inVendor := false
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line[0] != '\t' {
			if inVendor {
				break
			}
			if len(line) < 4 {
				continue
			}
			if strings.EqualFold(line[:4], vendor) {
				inVendor = true
				vName = strings.TrimSpace(line[4:])
			}
			continue
		}
		if !inVendor || device == "" || strings.HasPrefix(line, "\t\t") {
			continue
		}
		body := strings.TrimLeft(line, "\t")
		if len(body) < 4 {
			continue
		}
		if strings.EqualFold(body[:4], device) {
			dName = strings.TrimSpace(body[4:])
			return vName, dName
		}
	}
	return vName, dName
}
