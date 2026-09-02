# Fish completion for openbitdo.
# Install: copy this file to ~/.config/fish/completions/openbitdo.fish (or
# the equivalent system completions directory) — fish picks it up
# automatically in new shells, no sourcing needed.

complete -c openbitdo -l mock -d "Use mock transport/devices"
complete -c openbitdo -l debug-log -d "Write detailed protocol traces to a file" -r
complete -c openbitdo -l version -d "Print version and build information"
complete -c openbitdo -l diagnostics-dump -d "Print diagnostics reports without launching the TUI"
complete -c openbitdo -s h -l help -d "Print help"
