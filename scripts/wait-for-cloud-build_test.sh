#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -r -- "$test_root"' EXIT
mkdir -p "$test_root/bin"
cat >"$test_root/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1 $2 $3" == 'api -H Accept: application/vnd.github+json' ]]
printf '%s\n' "$4" >>"$FAKE_GH_LOG"
call=$(($(wc -l <"$FAKE_GH_LOG")))
responses=$(($(find "$FAKE_GH_RESPONSES" -name '*.json' | wc -l)))
response="$FAKE_GH_RESPONSES/$((call < responses ? call : responses)).json"
while [[ "$1" != "--jq" ]]; do shift; done
jq -r "$2" <"$response"
EOF
cat >"$test_root/bin/sleep" <<'EOF'
#!/usr/bin/env bash
EOF
chmod +x "$test_root/bin/gh" "$test_root/bin/sleep"

export PATH="$test_root/bin:$PATH"
export GITHUB_REPOSITORY=owner/repo GITHUB_SHA=0123456789abcdef0123456789abcdef01234567
check_name="abita-middleware-main-build (acuity-health-prod)"

check_run() {
  local name="$1" app="$2" status="$3" conclusion="$4"
  printf '{"name":"%s","app":{"name":"%s"},"status":"%s","conclusion":%s}' \
    "$name" "$app" "$status" "$conclusion"
}

run_case() {
  local label="$1" expected_exit="$2"
  shift 2
  export FAKE_GH_LOG="$test_root/$label.log" FAKE_GH_RESPONSES="$test_root/$label"
  mkdir -p "$FAKE_GH_RESPONSES"
  local call=1
  for runs in "$@"; do
    printf '{"check_runs":[%s]}' "$runs" >"$FAKE_GH_RESPONSES/$call.json"
    call=$((call + 1))
  done
  local actual_exit=0
  bash "$repo_root/scripts/wait-for-cloud-build.sh" "$check_name" >"$test_root/$label.out" 2>&1 || actual_exit=$?
  if [[ "$actual_exit" != "$expected_exit" ]]; then
    echo "$label: expected exit $expected_exit, got $actual_exit" >&2
    cat "$test_root/$label.out" >&2
    exit 1
  fi
}

other_app="$(check_run "$check_name" "Other App" completed '"success"')"
run_case waits-for-success 0 \
  '' \
  "$other_app" \
  "$other_app,$(check_run "$check_name" "Google Cloud Build" in_progress null)" \
  "$other_app,$(check_run "$check_name" "Google Cloud Build" completed '"success"')"
if (( $(wc -l <"$test_root/waits-for-success.log") != 4 )); then
  echo 'Expected four polls before success.' >&2
  exit 1
fi
if ! grep -Fq "repos/owner/repo/commits/$GITHUB_SHA/check-runs" "$test_root/waits-for-success.log"; then
  echo 'Did not query the check runs of the pushed commit.' >&2
  exit 1
fi

run_case failed-build 1 "$(check_run "$check_name" "Google Cloud Build" completed '"failure"')"
if ! grep -Fq 'conclusion: failure' "$test_root/failed-build.out"; then
  echo 'Failed build did not report its conclusion.' >&2
  exit 1
fi

run_case other-check-name 1 \
  "$(check_run "other-build (acuity-health-prod)" "Google Cloud Build" completed '"success"')" \
  "$(check_run "$check_name" "Google Cloud Build" completed '"cancelled"')"

export FAKE_GH_LOG="$test_root/missing-name.log"
if bash "$repo_root/scripts/wait-for-cloud-build.sh" 2>/dev/null; then
  echo 'Expected a missing check name to fail.' >&2
  exit 1
fi
if [[ -e "$FAKE_GH_LOG" ]]; then
  echo 'A missing check name reached GitHub.' >&2
  exit 1
fi
echo 'cloud build wait tests passed'
