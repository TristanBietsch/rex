package server

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func requireSessionID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("session_id required")
	}
	return nil
}

func validateNewSession(p protocol.NewSession) error {
	if strings.TrimSpace(p.ToolID) == "" {
		return fmt.Errorf("tool_id required")
	}
	if strings.TrimSpace(p.ModelID) == "" {
		return fmt.Errorf("model_id required")
	}
	if strings.TrimSpace(p.Slug) == "" {
		return fmt.Errorf("slug required")
	}
	if strings.TrimSpace(p.CWD) == "" {
		return fmt.Errorf("cwd required")
	}
	return nil
}
