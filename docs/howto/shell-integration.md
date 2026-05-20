# How to integrate with your shell

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Put `rex` on `PATH`, install completions, and show session status in your prompt.

## PATH

After `./install.sh`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Or append the block from:

```sh
./install.sh --shell-init >> ~/.zshrc
```

## Completions

```sh
rex completion zsh  >> ~/.zshrc
rex completion bash >> ~/.bashrc
rex completion fish >> ~/.config/fish/completions/rex.fish
```

Restart the shell.

## Prompt indicator

```sh
rex status
```

Exit **1** when a session needs input. Use in `PROMPT` or `precmd`:

```sh
# zsh example
precmd() {
  if ! rex status >/dev/null 2>&1; then
    RPROMPT='%F{red}rex⚡%f'
  else
    RPROMPT=''
  fi
}
```

## See also

- [cli.md](../cli.md) — `rex status` exit codes
- [paths.md](../paths.md) — install locations
