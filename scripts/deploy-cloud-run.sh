#!/usr/bin/env bash

set -euo pipefail

require_value() {
  local name="$1"
  local value="${!name:-}"
  if [[ -z "$value" || "$value" == "DO_NOT_DEPLOY" ]]; then
    echo "$name must be configured."
    exit 1
  fi
}

resume_scheduler_job() {
  local job="$1"
  local attempt
  local output

  for ((attempt = 1; attempt <= 6; attempt++)); do
    if output="$(
      gcloud scheduler jobs resume "$job" \
        "--project=$PROJECT_ID" \
        "--location=$REGION" \
        --quiet 2>&1
    )"; then
      if [[ -n "$output" ]]; then
        printf '%s\n' "$output"
      fi
      return 0
    fi

    printf '%s\n' "$output" >&2
    if [[ "$output" != *"ABORTED: parent resource not in ready state"* ]] ||
      ((attempt == 6)); then
      return 1
    fi

    echo "Scheduler backend is not ready; retrying resume in 5 seconds."
    sleep 5
  done
}

scheduler_job_exists() {
  gcloud scheduler jobs describe "$1" \
    "--project=$PROJECT_ID" \
    "--location=$REGION" >/dev/null 2>&1
}

pause_scheduler_job() {
  local job="$1"
  local scheduler_state

  scheduler_state="$(
    gcloud scheduler jobs describe "$job" \
      "--project=$PROJECT_ID" \
      "--location=$REGION" \
      "--format=value(state)"
  )"
  if [[ "$scheduler_state" != "PAUSED" ]]; then
    gcloud scheduler jobs pause "$job" \
      "--project=$PROJECT_ID" \
      "--location=$REGION" \
      --quiet
  fi
}

configure_paused_scheduler_job() {
  local job="$1"
  local schedule="$2"
  local path="$3"
  local attempt_deadline="$4"
  local max_retry_attempts="$5"
  local scheduler_args=(
    "$job"
    "--project=$PROJECT_ID"
    "--location=$REGION"
    "--schedule=$schedule"
    "--time-zone=Etc/UTC"
    "--uri=$MAINTENANCE_OIDC_AUDIENCE$path"
    "--http-method=POST"
    "--oidc-service-account-email=$MAINTENANCE_OIDC_SERVICE_ACCOUNT"
    "--oidc-token-audience=$MAINTENANCE_OIDC_AUDIENCE"
    "--attempt-deadline=$attempt_deadline"
    "--max-retry-attempts=$max_retry_attempts"
    "--min-backoff=30s"
    "--max-backoff=300s"
    "--quiet"
  )

  if scheduler_job_exists "$job"; then
    pause_scheduler_job "$job"
    gcloud scheduler jobs update http "${scheduler_args[@]}"
  else
    gcloud scheduler jobs create http "${scheduler_args[@]}"
    gcloud scheduler jobs pause "$job" \
      "--project=$PROJECT_ID" \
      "--location=$REGION" \
      --quiet
  fi
}

for name in \
  PROJECT_ID \
  REGION \
  REPOSITORY \
  IMAGE \
  SERVICE \
  RUNTIME_SERVICE_ACCOUNT \
  IMAGE_TAG \
  DEPLOYMENT_ID \
  DEPLOY_MODE; do
  require_value "$name"
done

if [[ ! "$IMAGE_TAG" =~ ^[0-9a-f]{40}$ ]]; then
  echo "IMAGE_TAG must be a full 40-character Git commit SHA."
  exit 1
fi

max_suffix_length=$((63 - ${#SERVICE} - 1))
if [[ ! "$DEPLOYMENT_ID" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] ||
  ((${#DEPLOYMENT_ID} > max_suffix_length)); then
  echo "DEPLOYMENT_ID must produce a valid Cloud Run revision name."
  exit 1
fi

tagged_image="$REGION-docker.pkg.dev/$PROJECT_ID/$REPOSITORY/$IMAGE:$IMAGE_TAG"
immutable_image="$(
  gcloud artifacts docker images describe "$tagged_image" \
    "--project=$PROJECT_ID" \
    "--format=value(image_summary.fully_qualified_digest)"
)"
if [[ ! "$immutable_image" =~ ^$REGION-docker\.pkg\.dev/$PROJECT_ID/$REPOSITORY/$IMAGE@sha256:[0-9a-f]{64}$ ]]; then
  echo "IMAGE_TAG did not resolve to the expected immutable image digest."
  exit 1
fi

revision="$SERVICE-$DEPLOYMENT_ID"
secrets="ADVANCEDMD_USERNAME=advancedmd-username:2,ADVANCEDMD_PASSWORD=advancedmd-password:2,ADVANCEDMD_OFFICE_KEY=advancedmd-office-key:2,ADVANCEDMD_APP_NAME=advancedmd-app-name:2,API_SECRET=middleware-api-secret:2,BOOKING_TOKEN_SECRET=booking-token-secret:2"
deploy_args=(
  "$SERVICE"
  "--project=$PROJECT_ID"
  "--region=$REGION"
  "--platform=managed"
  "--image=$immutable_image"
  "--revision-suffix=$DEPLOYMENT_ID"
  "--service-account=$RUNTIME_SERVICE_ACCOUNT"
  "--execution-environment=gen2"
  "--cpu=1"
  "--memory=512Mi"
  "--min=1"
  "--max=1"
  "--concurrency=20"
  "--timeout=60s"
  "--cpu-boost"
  "--port=8080"
  "--startup-probe=httpGet.path=/live,httpGet.port=8080"
  "--readiness-probe=httpGet.path=/ready,httpGet.port=8080"
  "--network=acuity-prod"
  "--subnet=cloud-run-us-east4"
  "--vpc-egress=all-traffic"
  "--ingress=all"
  "--no-invoker-iam-check"
  "--no-traffic"
  "--quiet"
)

case "$DEPLOY_MODE" in
  request)
    for name in \
      MAINTENANCE_OIDC_AUDIENCE \
      MAINTENANCE_OIDC_SERVICE_ACCOUNT \
      MAINTENANCE_SCHEDULE \
      MAINTENANCE_JOB \
      ACUITY_PRODUCT_URL \
      INSURANCE_REFRESH_JOB; do
      require_value "$name"
    done
    if [[ ! "$MAINTENANCE_OIDC_AUDIENCE" =~ ^https://[^/@?#]+$ ]]; then
      echo "MAINTENANCE_OIDC_AUDIENCE must be an HTTPS service base URL."
      exit 1
    fi
    if [[ ! "$MAINTENANCE_OIDC_SERVICE_ACCOUNT" =~ ^[^[:space:]@]+@[^[:space:]@]+\.iam\.gserviceaccount\.com$ ]]; then
      echo "MAINTENANCE_OIDC_SERVICE_ACCOUNT must be a service account email."
      exit 1
    fi
    if [[ ! "$ACUITY_PRODUCT_URL" =~ ^https://[^/@?#]+$ ]]; then
      echo "ACUITY_PRODUCT_URL must be an HTTPS service base URL."
      exit 1
    fi
    read -r minute hour day month weekday extra <<<"$MAINTENANCE_SCHEDULE"
    if [[ -n "${extra:-}" ||
      "$minute" != "0" ||
      ! "$hour" =~ ^\*/([0-9]+)$ ||
      "$day" != "*" ||
      "$month" != "*" ||
      "$weekday" != "*" ]]; then
      echo "MAINTENANCE_SCHEDULE must use '0 */N * * *'."
      exit 1
    fi
    interval_hours="${BASH_REMATCH[1]}"
    if ((interval_hours < 1 || interval_hours >= 20)); then
      echo "MAINTENANCE_SCHEDULE must run before the 20-hour stale threshold."
      exit 1
    fi
    secrets+=",ACUITY_PRODUCT_INSURANCE_TOKEN=acuity-product-insurance-token:1"
    deploy_args+=(
      "--cpu-throttling"
      "--set-env-vars=AMD_ENV=prod,MAINTENANCE_OIDC_AUDIENCE=$MAINTENANCE_OIDC_AUDIENCE,MAINTENANCE_OIDC_SERVICE_ACCOUNT=$MAINTENANCE_OIDC_SERVICE_ACCOUNT,ACUITY_PRODUCT_URL=$ACUITY_PRODUCT_URL"
    )
    ;;
  legacy-rollback)
    deploy_args+=(
      "--no-cpu-throttling"
      "--set-env-vars=AMD_ENV=prod"
    )
    ;;
  *)
    echo "DEPLOY_MODE must be request or legacy-rollback."
    exit 1
    ;;
esac

deploy_args+=("--set-secrets=$secrets")

gcloud run deploy "${deploy_args[@]}"

deployed_image="$(
  gcloud run revisions describe "$revision" \
    "--project=$PROJECT_ID" \
    "--region=$REGION" \
    "--format=value(spec.containers[0].image)"
)"
if [[ "$deployed_image" != "$immutable_image" ]]; then
  echo "Cloud Run revision does not use the expected immutable image."
  exit 1
fi

if [[ "$DEPLOY_MODE" == "legacy-rollback" ]]; then
  for job in "${MAINTENANCE_JOB:-}" "${INSURANCE_REFRESH_JOB:-}"; do
    if [[ -n "$job" ]] && scheduler_job_exists "$job"; then
      pause_scheduler_job "$job"
    fi
  done
else
  configure_paused_scheduler_job "$MAINTENANCE_JOB" "$MAINTENANCE_SCHEDULE" /ops/session/maintenance 60s 3
  configure_paused_scheduler_job "$INSURANCE_REFRESH_JOB" "* * * * *" /ops/insurance/refresh 30s 0
fi

gcloud run services update-traffic "$SERVICE" \
  "--project=$PROJECT_ID" \
  "--region=$REGION" \
  "--to-revisions=$revision=100" \
  --quiet

if [[ "$DEPLOY_MODE" == "request" ]]; then
  resume_scheduler_job "$MAINTENANCE_JOB"
  resume_scheduler_job "$INSURANCE_REFRESH_JOB"
fi
