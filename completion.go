package main

import (
	"fmt"
	"io"
	"os"
)

// printCompletion writes a shell completion script for the requested shell to w.
// Supported: bash, zsh, fish. Unknown shells produce an error message + non-zero exit.
//
// Invoked from `csm completion <shell>` — used by Homebrew's
// `generate_completions_from_executable` to drop completions into the right place
// at install time.
func printCompletion(w io.Writer, shell string) int {
	switch shell {
	case "bash":
		fmt.Fprint(w, bashCompletion)
	case "zsh":
		fmt.Fprint(w, zshCompletion)
	case "fish":
		fmt.Fprint(w, fishCompletion)
	default:
		fmt.Fprintf(os.Stderr, "csm: unsupported shell %q (want: bash, zsh, fish)\n", shell)
		return 1
	}
	return 0
}

const bashCompletion = `# bash completion for csm
_csm_completions() {
  local cur prev opts subs
  COMPREPLY=()
  cur="${COMP_WORDS[COMP_CWORD]}"
  prev="${COMP_WORDS[COMP_CWORD-1]}"
  opts="--print --lang --version --help"
  subs="version completion export merge download prune cleanup"

  case "$prev" in
    --lang)
      COMPREPLY=( $(compgen -W "en ko" -- "$cur") )
      return 0
      ;;
    completion)
      COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
      return 0
      ;;
  esac

  if [[ "$cur" == -* ]]; then
    COMPREPLY=( $(compgen -W "$opts" -- "$cur") )
  else
    COMPREPLY=( $(compgen -W "$opts $subs" -- "$cur") )
  fi
}
complete -F _csm_completions csm
`

const zshCompletion = `#compdef csm

_csm() {
  local -a flags subcommands
  flags=(
    '--print[Print selection to stdout, no exec]'
    '--lang[Force interface language]:lang:(en ko)'
    '--version[Show version splash]'
    '-v[Show version splash]'
    '--help[Show help]'
    '-h[Show help]'
  )
  subcommands=(
    'version:Show version splash'
    'completion:Print shell completion script'
    'export:Export a session as raw JSONL'
    'merge:Consolidate sessions via claude into the latest'
    'download:Bulk-export sessions'
    'prune:Trash sessions older than N days'
    'cleanup:Consolidate orphan sub-agent dirs'
  )

  _arguments -C \
    $flags \
    '1: :->cmd' \
    '*:: :->args'

  case $state in
    cmd)
      _describe -t commands 'csm subcommand' subcommands
      ;;
    args)
      case $words[1] in
        completion)
          _values 'shell' 'bash' 'zsh' 'fish'
          ;;
      esac
      ;;
  esac
}

_csm "$@"
`

const fishCompletion = `# fish completion for csm
complete -c csm -l print -d 'Print selection to stdout, no exec'
complete -c csm -l lang -d 'Force interface language' -xa 'en ko'
complete -c csm -l version -d 'Show version splash'
complete -c csm -s v -d 'Show version splash'
complete -c csm -l help -d 'Show help'
complete -c csm -s h -d 'Show help'

complete -c csm -n '__fish_use_subcommand' -a version -d 'Show version splash'
complete -c csm -n '__fish_use_subcommand' -a completion -d 'Print shell completion script'
complete -c csm -n '__fish_use_subcommand' -a export -d 'Export a session as raw JSONL'
complete -c csm -n '__fish_use_subcommand' -a merge -d 'Consolidate sessions via claude into the latest'
complete -c csm -n '__fish_use_subcommand' -a download -d 'Bulk-export sessions'
complete -c csm -n '__fish_use_subcommand' -a prune -d 'Trash sessions older than N days'
complete -c csm -n '__fish_use_subcommand' -a cleanup -d 'Consolidate orphan sub-agent dirs'

complete -c csm -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
`
