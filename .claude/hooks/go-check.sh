#!/usr/bin/env bash
# After Claude edits a .go file: gofmt it, then go vet its package.
# Exit 2 sends vet errors back to Claude.
set -uo pipefail

file=$(jq -r '.tool_input.file_path // empty')
[[ "$file" == *.go && -f "$file" ]] || exit 0

cd "$CLAUDE_PROJECT_DIR" || exit 0
mise exec -- gofmt -w "$file"

[[ -f go.mod ]] || exit 0
if ! out=$(mise exec -- go vet "$(dirname "$file")" 2>&1); then
  echo "$out" >&2
  exit 2
fi
