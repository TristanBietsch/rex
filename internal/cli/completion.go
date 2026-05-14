package cli

import (
	"flag"
	"fmt"
)

// RunCompletion prints a shell completion script.
func RunCompletion(args []string) error {
	fs := flag.NewFlagSet("completion", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return NewExitError(ExitInvalidArgs, err.Error())
	}
	if fs.NArg() != 1 {
		return NewExitError(ExitInvalidArgs, "completion: shell required (bash|zsh|fish)")
	}
	switch fs.Arg(0) {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		return NewExitError(ExitInvalidArgs, "supported shells: bash, zsh, fish")
	}
	return nil
}

const bashCompletion = `# rex bash completion. Source this from your ~/.bashrc:
#   source <(rex completion bash)
_rex_complete() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local verbs="status ls new attach reply send log wait rm rename archive reload daemon completion --version"
    COMPREPLY=( $(compgen -W "$verbs" -- "$cur") )
}
complete -F _rex_complete rex
`

const zshCompletion = `# rex zsh completion. Source this from your ~/.zshrc:
#   source <(rex completion zsh)
_rex() {
    local -a verbs
    verbs=(status ls new attach reply send log wait rm rename archive reload daemon completion --version)
    _describe 'verb' verbs
}
compdef _rex rex
`

const fishCompletion = `# rex fish completion. Source this:
#   rex completion fish | source
complete -c rex -f
complete -c rex -n "__fish_use_subcommand" -a "status ls new attach reply send log wait rm rename archive reload daemon completion --version"
`
