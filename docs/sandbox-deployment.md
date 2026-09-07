# Sandbox middleware

## Approved scope

Connect the AdvancedMD sandbox API account to a separate Cloud Run middleware
service, then route demo and staging agents to it. Verify credentials, synthetic
patient lookup, and availability only. No booking, patient creation, cancellation,
insurance update, production deployment, or production secret changes in this
setup. Agent integration is a separate PR.

One codebase, two services. Production remains unchanged. Demo and staging share
the sandbox service and its inventory, not production credentials or sessions.

## Provision and deploy

1. Verify the API credentials and application name against AdvancedMD. Admin
   website credentials are not the API credentials. Confirm that the office key
   belongs to the sandbox and that no other runtime owns this API login session.
2. Create the following secrets in the intended Google Cloud project. Never put
   values in Git, shell arguments, logs, PRs, or this document:

   | Secret | Runtime variable |
   | --- | --- |
   | `advancedmd-sandbox-username` | `ADVANCEDMD_USERNAME` |
   | `advancedmd-sandbox-password` | `ADVANCEDMD_PASSWORD` |
   | `advancedmd-sandbox-office-key` | `ADVANCEDMD_OFFICE_KEY` |
   | `advancedmd-sandbox-app-name` | `ADVANCEDMD_APP_NAME` |
   | `middleware-sandbox-api-secret` | `API_SECRET` |
   | `sandbox-booking-token-secret` | `BOOKING_TOKEN_SECRET` |

   Generate distinct random API and booking signing secrets. Grant secret access
   on these six secrets only to `abita-middleware-sandbox@PROJECT_ID.iam.gserviceaccount.com`.
   Do not grant it access to production secrets. Grant the deployer permission to
   use this identity and the selected network without expanding production IAM.
3. Create `middleware-sandbox-maintenance@PROJECT_ID.iam.gserviceaccount.com` as
   the dedicated maintenance caller identity. Do not grant it secret access. No
   Scheduler job is required initially: requests initialize/refresh the Session.
   Maintenance still requires a configured identity and the sandbox service's
   stable HTTPS base URL as its OIDC audience. For an existing sandbox service,
   read `status.url` with `gcloud run services describe abita-middleware-sandbox
   --project=PROJECT_ID --region=REGION --format='value(status.url)'`.
   For the first deployment, obtain the project number with
   `gcloud projects describe PROJECT_ID --format='value(projectNumber)'`, then use
   Google's documented deterministic URL:
   `https://abita-middleware-sandbox-PROJECT_NUMBER.REGION.run.app`.
   The service-name/project-number DNS label must be at most 63 characters; do not
   include a traffic tag. After deployment, resolve the service URL again and
   verify the configured audience is its supported stable endpoint. See
   [Cloud Run service URLs](https://docs.cloud.google.com/run/docs/triggering/https-request#deterministic).
   Never guess a hash-based hostname or reuse the production maintenance identity.
4. Build and test an immutable image from the reviewed sandbox branch. Use a
   digest, not `latest`; do not merge merely to trigger the production pipeline.
   Set the non-secret inputs below and run `bash scripts/deploy-sandbox-cloud-run.sh`:

   - `PROJECT_ID`, `REGION`
   - `SANDBOX_IMAGE`: project/region Artifact Registry `abita-middleware@sha256:...`
   - `SANDBOX_RUNTIME_SERVICE_ACCOUNT`: identity from step 2
   - `SANDBOX_NETWORK`, `SANDBOX_SUBNET`: approved existing Direct VPC network
     and subnet; preserve the provider-approved static egress path
   - `SANDBOX_MAINTENANCE_OIDC_AUDIENCE`: sandbox service HTTPS base URL
   - `SANDBOX_MAINTENANCE_OIDC_SERVICE_ACCOUNT`: identity from step 3
   - `SANDBOX_USERNAME_VERSION`, `SANDBOX_PASSWORD_VERSION`,
     `SANDBOX_OFFICE_KEY_VERSION`, `SANDBOX_APP_NAME_VERSION`,
     `SANDBOX_API_SECRET_VERSION`, `SANDBOX_BOOKING_SECRET_VERSION`: explicit
     positive integer secret versions

   The fixed target is `abita-middleware-sandbox`; the script cannot target the
   production service. It deploys `AMD_ENV=dev`, signed-token-only booking, 1 CPU,
   512 MiB, request-based billing, minimum zero and maximum one instance. It does
   not change production traffic, secrets, service accounts, or Scheduler jobs.
   Redeploy only while sandbox demos/tests are idle: revisions can briefly
   overlap, even with maximum one instance. Do not run another persistent
   middleware against the same API account.

Scale-to-zero is safe for this Session implementation: there is no background
refresh loop, and `Session.Get` performs bounded request-time login (up to 50
seconds). A cold request can exceed the agent's normal timeout. Warm with the
read-only smoke below before a scheduled demo, then start promptly; keeping an
instance warm permanently would be a separate cost decision. Existing VPC/NAT,
Secret Manager, logs, and active requests may still incur charges.

## Supported sandbox office

Use the middleware office selector `spring_hill` for sandbox calls. It resolves
to the existing dev Spring Hill facility/provider/column mappings. Confirm these
against sandbox inventory before declaring the integration ready; checked-in IDs
are not proof that inventory still exists.

Only the existing medical appointment translations are configured. Dev-mode
Crystal River production-ID placeholders and unconfigured vision translations
are rejected. Do not invent mappings or advertise unsupported specialties as
working. Production mappings are unchanged. Demo/staging agent routing must
explicitly override the middleware office selector and must fail closed if its
sandbox URL/token are missing; it must never fall back to production.

## Read-only verification gate

Keep API credentials, provider tokens, patient identifiers, and response bodies
out of logs and PRs. Use an already-provisioned, verified synthetic patient; do
not guess a real patient or create one in this setup.

1. Verify the exact deployed image digest, service account, secret names/versions,
   `AMD_ENV=dev`, network, CPU billing, and min/max instance settings. Confirm all
   service traffic goes to the intended sandbox revision.
2. `GET /live` and `GET /ready` should return 200. These prove process readiness,
   not that AdvancedMD credentials work.
3. Authenticate with the sandbox API bearer token and call
   `POST /api/patient/resolve` using
   `{"patientId":"<synthetic numeric ID>","office":"spring_hill"}`.
   Alternatively use `lastName` + `dob` for that synthetic patient. Do not combine
   `patientId` with lookup fields. Confirm the expected patient result privately.
4. Call `POST /api/scheduler/availability` with
   `{"office":"spring_hill","routing":"all_three","requestedDate":"<future YYYY-MM-DD>","dob":"<synthetic DOB>"}`.
   Confirm usable slots in the sandbox mapping, not just HTTP 200. An empty or
   provider-failed response does not prove availability works. Do not book.
5. Verify a missing API token returns 401. Connect the agent's sandbox middleware
   URL/token only after these checks succeed. Ensure production portal writes,
   notifications, and staff transfer effects are independently disabled for the
   sandbox agent route. No live call or simulation is needed for this setup gate.

Record sanitized results: image/revision, checks and outcome categories, count of
usable slots, and any unverified requirement. Do not call setup complete when
credentials work but synthetic data, mappings, or agent routing remain unverified.

Rollback is sandbox-only: stop demo/test traffic, deploy the previously verified
sandbox image digest and pinned secret versions with this script, then repeat
the read-only checks. Never fall back to the production service or credentials.

## GitHub staging deployments

`.github/workflows/staging.yml` deploys only the existing
`acuity-health-prod/us-east4/abita-middleware-sandbox`. It tests the exact Git SHA,
builds a context from `git archive` (excluding Actions credentials and local files),
pushes to the separate `middleware-sandbox` Artifact Registry repository, and
updates only the service image and `source-sha` label. Existing runtime settings,
secret versions, identity, network, and traffic allocation are checked before
and after the update. No production workflow, Cloud Build trigger, or production
IAM policy participates in this path.

Stop sandbox demos/tests and any other owner of the sandbox AdvancedMD session
before requesting a deployment. GitHub serializes these deployments, but cannot
detect an independently running developer process. Inspect recent sandbox
request logs before proceeding. Avoid simultaneous manual Cloud Run changes.

The on-demand entry point while this PR is unmerged is a unique `staging-*` tag:

```bash
# Replace REVIEWED_FULL_SHA with the reviewed commit. Tag creation confirms idle use.
git tag staging-YYYYMMDD-N REVIEWED_FULL_SHA
git push origin refs/tags/staging-YYYYMMDD-N
```

Creating the tag deploys that exact commit; it does not merge into `main`.
Never move or reuse a staging tag. After the workflow reaches the default branch,
GitHub also exposes **Actions → Deploy staging → Run workflow**, with an explicit
idle-session acknowledgement. Select `main`, `dev`, `codex/sandbox-middleware`,
or a `staging-*` tag. The selected ref is the source; there is no separate input
that can silently change the SHA associated with the GitHub deployment.
See [GitHub manual workflow requirements](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow).

Each run appears in the GitHub **staging** environment and uploads a sanitized
`staging-deployment.json` receipt, also included in the run summary. It records
the full source SHA, immutable image digest, previous/current revision, previous
image, `/live`, `/ready`, missing-token rejection, and API-token validation.
API-token validation sends invalid JSON, which is rejected before provider access.
A green deployment means ready service plus working middleware authentication;
synthetic patient lookup and usable provider availability remain explicitly
pending until an already verified live synthetic fixture is supplied and the
read-only verification gate above is completed. It does not mean a successful
provider workflow. Failed verification fails the GitHub deployment; it does not
silently roll back or declare the provider healthy. Use the previous image in
the receipt for a deliberate sandbox-only rollback, then repeat verification.

### CI identity and resource scope

The dedicated WIF pool is `middleware-staging`, provider `github`, project number
`1006405058436`. The deployer is
`middleware-staging-deploy@acuity-health-prod.iam.gserviceaccount.com`.
There are no service-account keys or personal Google credentials in Actions.
The provider requires repository ID `1131211316`, owner ID `247211163`, the exact
`repo:Data-Buddies-Solutions/amd_middleware:environment:staging` subject, this
workflow's path, and one of the allowed refs listed above. GitHub's staging
environment branch/tag policy enforces the same ref allowlist.

The deployer has only resource-level grants:

| Resource | Role |
| --- | --- |
| `abita-middleware-sandbox` Cloud Run service | `roles/run.developer` |
| `abita-middleware-sandbox` runtime service account | `roles/iam.serviceAccountUser` |
| `middleware-sandbox` Artifact Registry repository | `roles/artifactregistry.writer` |
| `middleware-sandbox-api-secret` secret | `roles/secretmanager.secretAccessor` |

The staging WIF subject has `roles/iam.workloadIdentityUser` on this deployer.
There are no project-wide grants to the deployer, no production resource grants,
and no changes to the pre-existing `github-actions` WIF pool/provider. The Security
Token Service API is enabled for OIDC exchange. The dedicated image repository
uses immutable tags, with source SHA, run ID, and attempt in every tag.

## Original deployment provenance, verified 2026-09-07

The original service was already running the PR #165 source; no source repair was
needed. Provenance was established from the archived build contents, not the
image tag alone:

| Evidence | Value |
| --- | --- |
| Git source | `e82f49fb3f0dd2fed416c7a80560e51dc2051cc5` |
| Cloud Build, `us-east4` | `1b5c1d55-3f7a-4cc6-9a6c-691373fa85b8` (SUCCESS, 2026-09-04) |
| Original image digest | `sha256:37c6426ac71725f0f738068b7fd7a42a19261252e41153fd96d240521bf94b34` |
| Original revision | `abita-middleware-sandbox-00001-vjr` |
| Source archive generation | `1788556711346414` |
| Source archive SHA-256 | `d840624b190f23c1248ae0a1ff204c3319d9126def4cd99a13cd38d4f72d14a8` |

The archive is
`gs://acuity-health-prod_us-east4_cloudbuild/source/1788556704.069481-b16af29b17fd482a86310ed501e12002.tgz`.
All 86 uploaded Git files matched this commit byte for byte; the sole omitted
tracked file was `.gitignore`, and there were no added or modified archive files.
Cloud Build's generation-pinned source hash matched the downloaded archive.
Its result digest matched the image used by the ready revision receiving 100%
of sandbox traffic. `/live`, `/ready`, missing-token 401, and authenticated
invalid-JSON rejection passed. No verified live synthetic patient was available,
so patient lookup and usable availability were not tested. This is historical
evidence; current image/revision proof lives in the staging deployment receipts.
