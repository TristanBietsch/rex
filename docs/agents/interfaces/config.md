# Configuration (agents)

Human reference: **[../../paths.md](../../paths.md)** (paths, env, daemon flags), **[../../settings.md](../../settings.md)** (config.yaml keys).

## Code entry points

| Concern | Package / file |
|---------|----------------|
| Settings registry | `internal/catalog/settings/registry.go` |
| Settings store | `internal/catalog/settings/store.go` |
| Default paths | `settings.DefaultPath()`, `daemonctl.DefaultStateDir()`, `daemonctl.DefaultSocket()`, `daemonctl.DefaultToolsPath()` |
| Daemon flags | `cmd/rex-daemon/main.go` |
| Socket dial default | `internal/surface/cli/core/socket.go` → `daemonctl.DefaultSocket()` |

## Settings vs daemon cap

- `max_concurrent_sessions` in YAML does not set daemon startup cap (`-max-concurrent-sessions` flag does).
- TUI settings UI can call `SetMaxConcurrent` live (`internal/surface/tui/settings.go`).

## Reload scope

- SIGHUP / `rex reload`: registry (`tools.yaml`) only.
- `config.yaml` and Lua: require daemon restart to re-read at startup.

## See also

- [modules/settings.md](../modules/settings.md)
- [modules/daemonctl.md](../modules/daemonctl.md)
- [modules/registry.md](../modules/registry.md)
