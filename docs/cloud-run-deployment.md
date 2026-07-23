# Cloud Run deployment

Production builds and deployments are deliberately separate because each new
Cloud Run revision starts the application and authenticates with AdvancedMD.

## Build

The `abita-middleware-main-build` trigger runs for every push to `main`:

1. Run `go test ./...`.
2. Build the production `Dockerfile`.
3. Push an immutable image tagged with the full Git commit SHA.

The build trigger cannot deploy Cloud Run or read runtime secrets.

## Deploy

Run the `abita-middleware-production-deploy` manual trigger with
`_IMAGE_TAG=<full 40-character commit SHA>`. The trigger rejects mutable tags
and abbreviated SHAs.

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

Do not use gradual traffic splitting. Deploy during low traffic, then verify:

1. Logs contain `Token refreshed successfully` and `Token manager started`.
2. `GET /health` returns `{"status":"ok"}`.
3. A synthetic read-only patient resolve returns HTTP 200.

Only after those checks should the agent's `AMD_API_URL` change.

## Rollback

If Cloud Run fails before the agent URL changes, delete the Cloud Run service
and restart Railway so Railway obtains a fresh AdvancedMD token.

If traffic has already moved, restore the Railway URL first, restart Railway,
verify `/health`, and then delete or stop Cloud Run.
