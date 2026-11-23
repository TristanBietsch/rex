package settings

import (
	"fmt"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// normalizeEnvPassthrough is the Normalize func of env_passthrough: empty, or
// comma-separated variable names, each optionally ending in "*". It applies
// the registry's env_pass rule, so an empty entry or a bare "*" is refused.
func normalizeEnvPassthrough(v string) (string, error) {
	if strings.TrimSpace(v) == "" {
		return "", nil
	}
	parts := strings.Split(v, ",")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if !registry.ValidEnvPassName(p) {
			return "", fmt.Errorf("%q must be a variable name, optionally ending in *", p)
		}
		parts[i] = p
	}
	return strings.Join(parts, ","), nil
}
