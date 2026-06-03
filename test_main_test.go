package main

import (
	"github.com/unxed/f4/vfs"
	"github.com/unxed/vtui"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	vfs.InitSudoClient("/usr/bin/f4", "")
	result := m.Run()
	if result != 0 {
		vtui.DumpLogsToFile("_failed_tests_f4.log")
	}
	os.Exit(result)
}
