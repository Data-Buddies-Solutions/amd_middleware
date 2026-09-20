#!/usr/bin/env bash
# Test the current middleware source against an exact compatible agent commit.
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
agent_ref="${1:-$(cat "$repo_root/tests/agent-contract-ref")}"
if [[ ! "$agent_ref" =~ ^[0-9a-f]{40}$ ]]; then
  echo "Agent contract requires an exact 40-character commit SHA." >&2
  exit 1
fi
agent_checkout="$(mktemp -d)"
trap 'rm -rf -- "$agent_checkout"' EXIT
git -C "$agent_checkout" init --quiet
git -C "$agent_checkout" fetch --quiet --depth=1 https://github.com/chasef07/abita_s2s.git "$agent_ref"
git -C "$agent_checkout" checkout --quiet --detach FETCH_HEAD
test "$(git -C "$agent_checkout" rev-parse HEAD)" = "$agent_ref"
echo "Agent contract commit: $agent_ref"
(cd "$agent_checkout" && uv sync --locked --python 3.13.15)
cd "$repo_root"
PYTHON_SCHEDULING_WORKTREE="$agent_checkout" go test -count=1 -run '^TestPython' -v ./internal/scheduling
