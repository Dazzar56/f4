//go:build windows

package main

import (
	"fmt"
	"os"
	"github.com/unxed/vtui"
)

func RunGui(backend string) error {
	return vtui.RunInGUIWindow(100, 30, backend, func() {
		SetupUI()
	})
}
