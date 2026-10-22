#!/usr/bin/env bash

# Waits for the named Google Cloud Build check run on $GITHUB_SHA to succeed.
set -euo pipefail

check_name="${1:?usage: wait-for-cloud-build.sh CHECK_NAME}"

deadline=$((SECONDS + 1200))
while (( SECONDS < deadline )); do
  check="$(
    CHECK_NAME="$check_name" gh api \
      -H "Accept: application/vnd.github+json" \
      "repos/$GITHUB_REPOSITORY/commits/$GITHUB_SHA/check-runs" \
      --jq '[
          .check_runs[]
          | select(
              .name == $ENV.CHECK_NAME
              and .app.name == "Google Cloud Build"
            )
        ]
        | first
        | if . == null then
            empty
          else
            [.status, (.conclusion // "")]
            | @tsv
          end'
  )"

  if [[ -z "$check" ]]; then
    echo "Waiting for the $check_name check to appear."
    sleep 10
    continue
  fi

  IFS=$'\t' read -r status conclusion <<< "$check"
  if [[ "$status" != "completed" ]]; then
    echo "Cloud Build is $status."
    sleep 10
    continue
  fi

  if [[ "$conclusion" == "success" ]]; then
    echo "Cloud Build $check_name succeeded for $GITHUB_SHA."
    exit 0
  fi

  echo "Cloud Build completed with conclusion: $conclusion" >&2
  exit 1
done

echo "Timed out waiting for Cloud Build to finish." >&2
exit 1
