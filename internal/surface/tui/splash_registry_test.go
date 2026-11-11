package tui

import (
	"strings"
	"testing"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

func TestRegistryLoadStatus_WarningsShowAsWarnStep(t *testing.T) {
	reg := &registry.Registry{
		Tools:    []registry.Tool{{ID: "claude"}},
		Warnings: []string{"tool gemini is no longer built in; define it fully in tools.yaml (docs/registry.md)"},
	}
	status, desc, err := registryLoadStatus(reg, nil, nil)
	if status != stepWarn || err != nil {
		t.Fatalf("status=%v err=%v; want warn, nil", status, err)
	}
	if !strings.Contains(desc, "tool gemini is no longer built in") {
		t.Errorf("desc %q does not carry the warning", desc)
	}
}

func TestRegistryLoadStatus_CleanIsOK(t *testing.T) {
	status, _, err := registryLoadStatus(&registry.Registry{Tools: []registry.Tool{{ID: "claude"}}}, nil, nil)
	if status != stepOK || err != nil {
		t.Fatalf("status=%v err=%v; want ok", status, err)
	}
}
