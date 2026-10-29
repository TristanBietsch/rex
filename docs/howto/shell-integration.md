# How to integrate with your shell

Prerequisite: [quickstart.md](../quickstart.md).

## Goal

Put `rex` on `PATH`, install completions, and show session status in your prompt.

## PATH

After `./install.sh`:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Or append the block `--shell-init` prints for your login shell (`$SHELL`). It puts the install's bin directory on `PATH` and loads completions:

```sh
./install.sh --shell-init >> ~/.zshrc                      # zsh
./install.sh --shell-init >> ~/.bashrc                     # bash
./install.sh --shell-init >> ~/.config/fish/config.fish    # fish
```

For a shell other than `$SHELL`, set it: `SHELL=bash ./install.sh --shell-init`. The zsh block loads completion only when `compinit` has already run, so append it after `compinit` (or after your framework's init, such as oh-my-zsh).

## Completions

To load completions without the block:

```sh
source <(rex completion zsh)                              # in ~/.zshrc, after compinit
eval "$(rex completion bash)"                             # in ~/.bashrc
rex completion fish > ~/.config/fish/completions/rex.fish
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
