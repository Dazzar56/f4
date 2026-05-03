//go:build windows

package vfs

import (
	"github.com/unxed/vtui"
	"golang.org/x/sys/windows"
)

func init() {
	// Enable backup/restore privileges to allow entering protected directories
	// like "System Volume Information" if the user has administrative rights.
	enablePrivilege("SeBackupPrivilege")
	enablePrivilege("SeRestorePrivilege")
}

func enablePrivilege(name string) {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token)
	if err != nil {
		return
	}
	defer token.Close()

	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid)
	if err != nil {
		return
	}

	privs := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED},
		},
	}

	err = windows.AdjustTokenPrivileges(token, false, &privs, 0, nil, nil)
	if err != nil {
		vtui.DebugLog("VFS: Failed to enable privilege %s: %v", name, err)
	} else {
		vtui.DebugLog("VFS: Successfully enabled privilege %s", name)
	}
}
