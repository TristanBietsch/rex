# How to add a tool to the registry

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Register a new agent CLI in `tools.yaml` and spawn a session with it.

## Steps

1. Create or edit `~/.config/rex/tools.yaml`:

```yaml
tools:
  - id: mycli
    name: My CLI
    category: self_hosted
    command: [mycli, --headless]
    detect:
      kind: heuristic
      prompt_regex: "(?m)^> "
      idle_ms: 1000
    icon: "●"
    color: "#666666"
    models:
      - id: default
        name: Default
        args: []
```

2. Reload the registry:

```sh
rex reload
```

3. Spawn:

```sh
rex new "smoke test" --tool mycli --model default --slug mycli-smoke
```

4. Confirm:

```sh
rex ls --tool mycli
```

## Merge behavior

Your file merges into the built-in catalog by tool `id`. Existing builtins with the same `id` are replaced field by field. Models merge by model `id`.

See [registry.md](../registry.md) for `detect` kinds and validation rules.

## See also

- [settings.md](../settings.md) — `default_spawn_tool` for TUI `i` key
