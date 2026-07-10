//go:build windows

package main

import (
	"os"
	"strings"

	"github.com/unxed/vtui"
)

func RunGui(backend string) error {
	if backend == "qt" || strings.HasPrefix(backend, "ext:") {
		return RunExternalUIWithMapping(backend)
	}
	// DX12: use naga DXIL backend instead of HLSL→FXC
	// to avoid 2-6s shader compilation via d3dcompiler_47.dll
	if os.Getenv("GOGPU_DX12_DXIL") == "" {
		api := os.Getenv("GOGPU_GRAPHICS_API")
		if api == "" || strings.EqualFold(api, "dx12") || strings.EqualFold(api, "d3d12") || strings.EqualFold(api, "directx") {
			os.Setenv("GOGPU_DX12_DXIL", "1")
		}
	}
	return vtui.RunInGUIWindow(100, 30, backend, func() {
		SetupUI()
	})
}
