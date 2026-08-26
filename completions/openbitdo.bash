# Bash completion for openbitdo.
# Install: source this file (e.g. from ~/.bashrc), or copy it into your
# bash-completion directory (/etc/bash_completion.d/ on Linux, or
# $(brew --prefix)/etc/bash_completion.d/ on a Homebrew macOS install), then
# open a new shell.

_openbitdo() {
    local cur prev opts
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD - 1]}"
    opts="--mock --debug-log --help -h"

    case "$prev" in
        --debug-log)
            COMPREPLY=($(compgen -f -- "$cur"))
            return 0
            ;;
    esac

    COMPREPLY=($(compgen -W "$opts" -- "$cur"))
    return 0
}
complete -F _openbitdo openbitdo
