//go:build windows

package main

import "golang.org/x/sys/windows"

func isAdmin() bool {
	return windows.IsUserAnAdmin()
}

func getAdminString() string {
	if isAdmin() {
		return "Administrator"
	}
	return ""
}
