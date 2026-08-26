#compdef openbitdo
# Zsh completion for openbitdo.
# Install: place this file as _openbitdo somewhere on your $fpath (e.g.
# ~/.zsh/completions/_openbitdo, with that directory added to fpath before
# compinit runs in your ~/.zshrc), then open a new shell.

_openbitdo() {
    _arguments \
        '--mock[Use mock transport/devices]' \
        '--debug-log[Write detailed protocol traces to a file]:log file:_files' \
        '(-h --help)'{-h,--help}'[Print help]'
}

_openbitdo "$@"
