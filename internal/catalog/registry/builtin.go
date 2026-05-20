package registry

import _ "embed"

//go:embed builtin.yaml
var builtinYAML []byte

// BuiltinBytes returns the embedded built-in registry YAML.
func BuiltinBytes() []byte { return builtinYAML }
