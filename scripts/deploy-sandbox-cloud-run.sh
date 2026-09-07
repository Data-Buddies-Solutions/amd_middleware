#!/usr/bin/env bash

# Deliberately separate from the automatic production deployment pipeline.
set -euo pipefail

for name in \
  PROJECT_ID REGION SANDBOX_IMAGE SANDBOX_RUNTIME_SERVICE_ACCOUNT \
  SANDBOX_NETWORK SANDBOX_SUBNET \
  SANDBOX_MAINTENANCE_OIDC_AUDIENCE SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT \
  SANDBOX_USERNAME_VERSION SANDBOX_PASSWORD_VERSION SANDBOX_OFFICE_KEY_VERSION \
  SANDBOX_APP_NAME_VERSION SANDBOX_API_SECRET_VERSION SANDBOX_BOOKING_SECRET_VERSION; do
  if [[ -z "${!name:-}" || "${!name}" == "DO_NOT_DEPLOY" ]]; then
    echo "$name must be configured." >&2
    exit 1
  fi
done

if [[ ! "$SANDBOX_IMAGE" =~ ^$REGION-docker\.pkg\.dev/$PROJECT_ID/[a-z0-9_-]+/abita-middleware@sha256:[0-9a-f]{64}$ ]]; then
  echo "SANDBOX_IMAGE must be an immutable abita-middleware digest in this project and region." >&2
  exit 1
fi
if [[ "$SANDBOX_RUNTIME_SERVICE_ACCOUNT" != "abita-middleware-sandbox@$PROJECT_ID.iam.gserviceaccount.com" ||
  "$SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT" != "middleware-sandbox-maintenance@$PROJECT_ID.iam.gserviceaccount.com" ]]; then
  echo "Sandbox requires its dedicated runtime and maintenance service accounts." >&2
  exit 1
fi
if [[ ! "$SANDBOX_MAINTENANCE_OIDC_AUDIENCE" =~ ^https://abita-middleware-sandbox-[a-z0-9.-]+\.run\.app$ ]]; then
  echo "Sandbox maintenance audience must be this sandbox service's HTTPS Cloud Run base URL." >&2
  exit 1
fi
for name in \
  SANDBOX_USERNAME_VERSION SANDBOX_PASSWORD_VERSION SANDBOX_OFFICE_KEY_VERSION \
  SANDBOX_APP_NAME_VERSION SANDBOX_API_SECRET_VERSION SANDBOX_BOOKING_SECRET_VERSION; do
  if [[ ! "${!name}" =~ ^[1-9][0-9]*$ ]]; then
    echo "$name must be an explicit numeric Secret Manager version." >&2
    exit 1
  fi
done

secret_bindings="ADVANCEDMD_USERNAME=advancedmd-sandbox-username:$SANDBOX_USERNAME_VERSION"
secret_bindings+=",ADVANCEDMD_PASSWORD=advancedmd-sandbox-password:$SANDBOX_PASSWORD_VERSION"
secret_bindings+=",ADVANCEDMD_OFFICE_KEY=advancedmd-sandbox-office-key:$SANDBOX_OFFICE_KEY_VERSION"
secret_bindings+=",ADVANCEDMD_APP_NAME=advancedmd-sandbox-app-name:$SANDBOX_APP_NAME_VERSION"
secret_bindings+=",API_SECRET=middleware-sandbox-api-secret:$SANDBOX_API_SECRET_VERSION"
secret_bindings+=",BOOKING_TOKEN_SECRET=sandbox-booking-token-secret:$SANDBOX_BOOKING_SECRET_VERSION"

# No Scheduler is created: request-time Session.Get owns cold-start recovery.
# A dedicated maintenance identity is still required by runtime configuration.
# Stop other sandbox API session owners before deploying/redeploying this service.
gcloud run deploy abita-middleware-sandbox \
  "--project=$PROJECT_ID" \
  "--region=$REGION" \
  --platform=managed \
  "--image=$SANDBOX_IMAGE" \
  "--service-account=$SANDBOX_RUNTIME_SERVICE_ACCOUNT" \
  --execution-environment=gen2 \
  --cpu=1 --memory=512Mi --min=0 --max=1 --concurrency=20 --timeout=60s \
  --cpu-throttling --cpu-boost --port=8080 \
  --startup-probe=httpGet.path=/live,httpGet.port=8080 \
  --readiness-probe=httpGet.path=/ready,httpGet.port=8080 \
  "--set-secrets=$secret_bindings" \
  "--set-env-vars=AMD_ENV=dev,ALLOW_RAW_SLOT_BOOKING=false,MAINTENANCE_OIDC_AUDIENCE=$SANDBOX_MAINTENANCE_OIDC_AUDIENCE,MAINTENANCE_OIDC_SERVICE_ACCOUNT=$SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT" \
  "--network=$SANDBOX_NETWORK" \
  "--subnet=$SANDBOX_SUBNET" \
  --vpc-egress=all-traffic --ingress=all --no-invoker-iam-check --quiet

deployed_image="$(gcloud run services describe abita-middleware-sandbox \
  "--project=$PROJECT_ID" "--region=$REGION" \
  '--format=value(spec.template.spec.containers[0].image)')"
if [[ "$deployed_image" != "$SANDBOX_IMAGE" ]]; then
  echo "Sandbox service does not reference the requested immutable image." >&2
  exit 1
fi
echo "Sandbox deployment submitted and image verified; authenticated provider smoke is still required."
