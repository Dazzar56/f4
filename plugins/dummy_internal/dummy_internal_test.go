package dummy_internal

import (
	"github.com/unxed/f4/vfs"
	"testing"
)

type mockHostAPI struct {
	vfs.HostAPI
	versionCalled bool
	logCalled     bool
	logMsg        string
}

func (m *mockHostAPI) GetVersion() string {
	m.versionCalled = true
	return "v1.0.0-mock"
}

func (m *mockHostAPI) Log(msg string) {
	m.logCalled = true
	m.logMsg = msg
}

func TestInternalDummyPlugin(t *testing.T) {
	p := &InternalDummyPlugin{}

	if p.GetName() != "Internal Hello World" {
		t.Errorf("Unexpected plugin name: %q", p.GetName())
	}

	api := &mockHostAPI{}
	err := p.Init(api)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if !api.versionCalled {
		t.Error("Init did not call GetVersion")
	}
	if !api.logCalled {
		t.Error("Init did not call Log")
	}
	expectedLog := "Internal dummy plugin initialized! Host Version: v1.0.0-mock"
	if api.logMsg != expectedLog {
		t.Errorf("Expected log message %q, got %q", expectedLog, api.logMsg)
	}

	err = p.Close()
	if err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}
