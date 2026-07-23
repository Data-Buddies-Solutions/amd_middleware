# Cloud Run deployment

Production deploys are automatic after the required tests and image build
succeed. Each `main` build is one sequential pipeline, so a failed test, build,
or image push cannot change Cloud Run.

## Automatic production pipeline

The `abita-middleware-main-build` trigger runs for every push to `main`:

1. Run `go test ./...`.
2. Build the production `Dockerfile`.
3. Push an immutable image tagged with the full Git commit SHA.
4. Deploy that exact image to Cloud Run.

The trigger can deploy Cloud Run but cannot read runtime secret values. The
runtime service account reads the pinned Secret Manager versions when the
container starts.

## Manual redeploy

Run the `abita-middleware-production-deploy` manual trigger with
`_IMAGE_TAG=<full 40-character commit SHA>` to redeploy or roll back to an image
that the automatic pipeline already built. The trigger rejects mutable tags and
abbreviated SHAs.

The deploy trigger creates one production revision with:

- region `us-east4`
- 1 vCPU and 512 MiB memory
- minimum 1 and maximum 1 instance
- always-allocated CPU
- concurrency 20
- 60-second request timeout
- `/health` startup and readiness probes
- Direct VPC egress through `acuity-prod` and `cloud-run-us-east4`
- public access with the Cloud Run invoker IAM check disabled
- explicit Secret Manager versions

Do not use gradual traffic splitting. After a deployment, verify:

1. Logs contain `Token refreshed successfully` and `Token manager started`.
2. `GET /health` returns `{"status":"ok"}`.
3. A synthetic read-only patient resolve returns HTTP 200.

## Rollback

Run the manual trigger with the last known-good full commit SHA. If Cloud Run
itself is unavailable, reconnect and restart Railway, restore the Railway URL in
the agent, and verify `/health`. Never leave Railway and Cloud Run running as
long-lived AdvancedMD token owners.
