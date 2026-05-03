package main

import (
	"github.com/unxed/vtui"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	result := m.Run()
	if result != 0 {
		vtui.DumpLogsToFile("_failed_tests_f4.log")
	}
	os.Exit(result)
}
