#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -r -- "$test_root"' EXIT
mkdir -p "$test_root/bin"
cat >"$test_root/bin/gcloud" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"$FAKE_GCLOUD_LOG"
case "$*" in
  'run deploy abita-middleware-sandbox '*) ;;
  'run services describe abita-middleware-sandbox '*) printf '%s\n' "$SANDBOX_IMAGE" ;;
  *) echo 'Unexpected cloud action' >&2; exit 1 ;;
esac
EOF
chmod +x "$test_root/bin/gcloud"

export PATH="$test_root/bin:$PATH"
export FAKE_GCLOUD_LOG="$test_root/gcloud.log"
export PROJECT_ID=test-project REGION=us-east4
export SANDBOX_IMAGE="us-east4-docker.pkg.dev/test-project/services/abita-middleware@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
export SANDBOX_RUNTIME_SERVICE_ACCOUNT=abita-middleware-sandbox@test-project.iam.gserviceaccount.com
export SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT=middleware-sandbox-maintenance@test-project.iam.gserviceaccount.com
export SANDBOX_MAINTENANCE_OIDC_AUDIENCE=https://abita-middleware-sandbox-123456.us-east4.run.app
export SANDBOX_NETWORK=test-network SANDBOX_SUBNET=test-subnet
export SANDBOX_USERNAME_VERSION=1 SANDBOX_PASSWORD_VERSION=2 SANDBOX_OFFICE_KEY_VERSION=3
export SANDBOX_APP_NAME_VERSION=4 SANDBOX_API_SECRET_VERSION=5 SANDBOX_BOOKING_SECRET_VERSION=6

bash "$repo_root/scripts/deploy-sandbox-cloud-run.sh"
for expected in \
  '--min=0 --max=1' '--cpu-throttling' \
  'AMD_ENV=dev,ALLOW_RAW_SLOT_BOOKING=false' \
  'ADVANCEDMD_USERNAME=advancedmd-sandbox-username:1' \
  'ADVANCEDMD_PASSWORD=advancedmd-sandbox-password:2' \
  'ADVANCEDMD_OFFICE_KEY=advancedmd-sandbox-office-key:3' \
  'ADVANCEDMD_APP_NAME=advancedmd-sandbox-app-name:4' \
  'API_SECRET=middleware-sandbox-api-secret:5' \
  'BOOKING_TOKEN_SECRET=sandbox-booking-token-secret:6' \
  '--network=test-network --subnet=test-subnet --vpc-egress=all-traffic'; do
  if ! grep -Fq -- "$expected" "$FAKE_GCLOUD_LOG"; then
    echo "Missing sandbox deployment flag: $expected" >&2
    exit 1
  fi
done
if grep -Eq 'scheduler|AMD_ENV=prod|=advancedmd-password:|run deploy abita-middleware ' "$FAKE_GCLOUD_LOG"; then
  echo 'Sandbox deployment touched production or Scheduler configuration.' >&2
  exit 1
fi

reject_before_cloud() {
  local name="$1" value="$2"
  local before after
  before="$(wc -l <"$FAKE_GCLOUD_LOG")"
  if env "$name=$value" bash "$repo_root/scripts/deploy-sandbox-cloud-run.sh"; then
    echo "Expected invalid $name to fail." >&2
    exit 1
  fi
  after="$(wc -l <"$FAKE_GCLOUD_LOG")"
  if [[ "$before" != "$after" ]]; then
    echo "Invalid $name reached a cloud command." >&2
    exit 1
  fi
}
reject_before_cloud SANDBOX_IMAGE us-east4-docker.pkg.dev/test-project/services/abita-middleware:latest
reject_before_cloud SANDBOX_RUNTIME_SERVICE_ACCOUNT abita-middleware@test-project.iam.gserviceaccount.com
reject_before_cloud SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT production@test-project.iam.gserviceaccount.com
reject_before_cloud SANDBOX_MAINTENANCE_OIDC_AUDIENCE https://abita-middleware-123456.us-east4.run.app
reject_before_cloud SANDBOX_PASSWORD_VERSION latest
reject_before_cloud SANDBOX_API_SECRET_VERSION ''
reject_before_cloud SANDBOX_NETWORK ''
echo 'sandbox deployment isolation tests passed'
