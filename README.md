# Abita Middleware

**Safe patient and scheduling workflows between Acuity's voice agent and the
clinical system of record.**

The caller asks for outcomes: find this patient, offer a valid appointment, book this
slot, cancel this visit. The middleware owns everything that makes those outcomes safe:
authentication, office and insurance policy, eligibility, concurrency checks, provider
translation, and recovery when a write may or may not have succeeded. It is one Go
deployable, organized as a modular monolith.

**Agent-friendly means a correct edit to one file preserves the whole service's
invariants.** The design below exists so that an edit in the right folder cannot break a
rule somewhere else.

## Six nouns organize the service

Each noun has one place in the tree and one job at runtime.

| Noun | Place | Job |
| --- | --- | --- |
| **Handler** | `internal/http/` | Decode one request, call one feature, respond. Records the PHI-safe request outcome. Never decides policy. |
| **Feature** | `internal/patient/`, `internal/scheduling/`, `internal/eligibility/`, `internal/insurance/` | The one owner of a workflow's rules, receipts, and reconciliation. Commands and results carry their own JSON tags. |
| **Value** | `internal/domain/`, `internal/safeerrors/`, `internal/safelog/` | Shared offices, routing, patient and scheduler types, and PHI-safe errors and logs. No I/O. |
| **Records** | `internal/advancedmd/` | The `PatientRecords` and `SchedulingRecords` seam, the production adapter, the deterministic test adapter, and the one classifier of failed writes. |
| **Transport** | `internal/clients/`, `internal/session/` | AdvancedMD XMLRPC and REST calls, the one response decoder, and the one owner of the login token. |
| **Composition** | `cmd/api/`, `internal/config/` | Reads the environment once and wires one of everything. |

## Layers are visible in the tree

A package's folder tells you where it sits and which imports are legal.

```mermaid
flowchart LR
  agent(["Voice agent"]) --> http
  composition["cmd/api · config<br/>composition"] --> http
  subgraph handler["Handler"]
    http["http<br/>decode · call · respond"]
  end
  subgraph features["Features"]
    workflows["patient · scheduling · eligibility"]
    insurance["insurance<br/>policy, no I/O"]
  end
  subgraph seam["Records seam"]
    records["advancedmd<br/>PatientRecords · SchedulingRecords"]
  end
  subgraph transport["Transport"]
    clients["clients<br/>decodeXMLRPC · oneOrMany"]
    session["session<br/>one login token"]
  end
  http --> workflows
  workflows --> insurance
  workflows --> records
  records --> clients
  clients --> session
  clients --> amd[("AdvancedMD")]
  workflows --> stedi[("Stedi")]
```

Every layer may use the values (`domain`, `safeerrors`, `safelog`), which import nothing
internal.

| Layer | Packages | May import |
| --- | --- | --- |
| Value | `domain`, `safeerrors`, `safelog` | nothing internal |
| Session | `session` | values |
| Transport | `clients` | values, session |
| Records | `advancedmd`, `advancedmdtest` | values, session, transport |
| Policy | `insurance` | values |
| Feature | `patient`, `scheduling`, `eligibility` | values, records, policy. Never another feature. |
| Handler | `http` | values, session, records, policy, features. Never transport. |
| Composition | `cmd/api`, `config` | anything |

[layers_test.go](layers_test.go) assigns every package a layer and fails on an import that
points the wrong way. A new package fails the test until it is given a layer.

## Feature blueprint

A feature is a vertical slice. The request shape, the rules, the provider seam, and the
wire format each live in their named owner.

```mermaid
flowchart LR
  handler["<b>Handler</b><br/>http/handlers.go<br/>decode · call · respond"]
  feature["<b>Feature</b><br/>scheduling/<br/>rules · signed tokens · receipts"]
  records["<b>Records</b><br/>advancedmd/<br/>SchedulingRecords · adapter<br/>write classification"]
  transport["<b>Transport</b><br/>clients/ · session/<br/>request · decode · token"]
  handler <--> feature <--> records <--> transport
```

**The handler never handles provider calls, retries, reconciliation, or token state.**

To add a capability:

1. Put the rule in the feature that owns it. Give its command and result JSON tags so the
   handler can decode and encode them directly.
2. If it needs AdvancedMD data, add a method to `PatientRecords` or `SchedulingRecords`
   and implement it in [adapter.go](internal/advancedmd/adapter.go) and
   [advancedmdtest](internal/advancedmd/advancedmdtest/adapter.go).
3. Parse the provider response in `clients` with `decodeXMLRPC` and `oneOrMany`, and
   return domain types.
4. Add a handler that decodes (`decodeStrict` for new endpoints), calls the feature, and
   calls `respond`. Register the route in [router.go](internal/http/router.go).
5. Read any new environment variable in [config](internal/config/config.go) and wire the
   dependency in [main.go](cmd/api/main.go).

## Decisions

- **One writer per state.** `session` owns the login token, `patient` owns patient
  mutations, `scheduling` owns appointment writes and signed tokens, and
  `advancedmd.classifyMutation` alone decides whether a failed write is definite or
  ambiguous.
- **Writes are sent once.** An ambiguous write is reconciled through an authoritative
  read or returned as `indeterminate_write`, never retried automatically. See
  [Write safety](#write-safety).
- **Complete reads prove absence.** A record missing from a partial read is unknown, not
  absent. Malformed occupancy fails its column; an incomplete appointment read is an error,
  not "none found".
- **A slot is a signed promise.** Slot, cancellation, and reschedule tokens share one
  HMAC signer in [tokens.go](internal/scheduling/tokens.go). Booking revalidates the signed
  facts against live state. `TestTokenFormatIsStable` pins the token bytes so in-flight
  tokens survive a deploy.
- **Refresh is bounded.** Scheduler setup waits honor cancellation, and a failed refresh
  can reuse cached setup for at most 24 hours. Authentication failures respect the retry
  cooldown.
- **Config owns the environment.** Only [config](internal/config/config.go) reads
  environment variables.
- **Response shapes are a contract with the voice agent.** A change to a request or
  response shape ships together with the agent that reads it.
- **Observability is PHI-safe.** Logs carry route, status, safe category, latency, and a
  redacted request ID, never bodies, patient IDs, tokens, or raw provider errors.
- **No code comments.** Intent lives in names and types.
  [comments_test.go](comments_test.go) allows only `//go:` directives.

## Write safety

Network failure is not proof that a write failed. The provider may have applied
the mutation before the connection disappeared. Patient and Scheduling
therefore use the same recovery shape:

```mermaid
flowchart TD
    command["Validated command"] --> prepare["Read the state needed to prove the effect"]
    prepare --> write["Attempt the mutation once"]
    write -->|Definitive success| receipt["Return the normal receipt"]
    write -->|Definitive rejection| failure["Return a stable failure"]
    write -->|Ambiguous result| reconcile["Read authoritative state"]
    reconcile -->|Effect proven| receipt
    reconcile -->|Effect disproven by a complete read| failure
    reconcile -->|Read failed or incomplete| unknown["indeterminate_write<br/>Do not retry automatically"]
```

Examples of authoritative proof:

- Patient creation compares the pre-write patient baseline with exact
  post-write matches.
- Insurance replacement requires patient ID and DOB, loads fresh demographics,
  verifies DOB, and derives the current primary plan and responsible party.
  Legacy `insPlanId`, `respPartyId`, and `oldInsurance` inputs are ignored.
  A known empty current plan attaches directly; an already-matching active
  replacement performs no writes. Missing references block the operation.
- Booking reads the intended appointment month and matches the patient, office,
  time, provider, and appointment type.
- Cancellation reads the original appointment's owning month and proves
  whether it still exists.

Insurance update responses retain `status: updated|error` and diagnostic `outcome`,
and add `effect: no_effect|completed|partial|uncertain`. `no_effect` proves no
change from this request; `completed` proves the requested insurance is active;
`partial` means the old plan ended but the replacement was not attached;
`uncertain` means a possible effect could not be reconciled. Partial and uncertain
results require staff recovery, never an automatic repeat of end/add.

`POST /api/scheduler/slots` lists openings without requiring chart insurance
clearance. Office, visit type, age, and requested routing select providers;
routine vision defaults to optical routing. Booking verifies patient identity,
the signed slot, appointment policy, and live occupancy without re-triaging chart
insurance. Insurance acceptance remains part of registration and insurance updates.

`POST /api/scheduler/slots` errors preserve the inventory envelope with `slots: []`.
`invalid_input` requires corrected input and never retries the same search.
`availability_search_incomplete` is a read failure, permits one retry, and then
requires staff help; it never proves there are no openings.

Cancellation `provider_rejected` and `provider_conflict` are definitive failures,
not uncertain writes. Refresh appointments before a new action. Validation,
invalid-token, and ownership failures perform no cancellation. Legacy validation
responses may omit the wire outcome; consumers must treat an unclassified error
conservatively rather than infer no effect. `write_failed`
means a pre-write failure or a reconciled failed write; `indeterminate_write`
means the cancellation may have happened and must not be retried automatically.
A `cancelled` receipt identifies the exact appointment that was cancelled.

## The scheduling handshake

Availability and booking are deliberately one workflow. A slot can become
invalid after it is offered, so booking never trusts an old search result by
itself.

```mermaid
sequenceDiagram
    participant C as Caller
    participant S as Scheduling
    participant D as Scheduling policy
    participant R as SchedulingRecords

    C->>S: Search(date, office, routing, DOB)
    S->>D: Resolve eligible columns and rules
    S->>R: Read live appointments and holds
    S-->>C: Slots + signed bookingToken
    C->>S: Book(patient intent + bookingToken)
    S->>R: Re-read patient, setup, appointments, and holds
    S->>D: Revalidate office, type, provider, capacity, and force
    alt Facts still match
        S->>R: Attempt booking once
        R-->>S: Definitive or ambiguous result
        S-->>C: Receipt, stable failure, or indeterminate_write
    else Facts changed
        S-->>C: slot_unavailable
    end
```

This prevents a stale slot, changed patient context, or changed capacity
decision from silently becoming a booking.

## Session lifecycle

The process keeps one session in memory. `Get` performs request-time recovery;
`Maintain` supports authenticated proactive maintenance; `Status` reports
lifecycle state without exposing credentials, tokens, or provider URLs.

```mermaid
stateDiagram-v2
    [*] --> Uninitialized
    Uninitialized --> Refreshing: first Get or Maintain
    Fresh --> Stale: age reaches recovery threshold
    Fresh --> Refreshing: Maintain
    Stale --> Refreshing: Get or Maintain
    Degraded --> Refreshing: retry window passes
    Refreshing --> Fresh: login succeeds
    Refreshing --> Degraded: login fails, prior session still usable
    Refreshing --> Unavailable: login fails, no usable session
    Degraded --> Unavailable: prior session expires
    Unavailable --> Refreshing: next Get or Maintain
```

Concurrent callers share one in-flight login. A refresh failure may degrade the
session while a last-known-good token remains usable; an expired or missing
token makes the session unavailable.

## HTTP interface

All `/api/*` routes require `Authorization: Bearer <API_SECRET>`.

| Route | Intent | Owner |
| --- | --- | --- |
| `POST /api/patient/resolve` | Resolve identity, demographics, routing, and upcoming appointments | `patient.Resolve` |
| `POST /api/add-patient` | Create a patient and attach primary insurance | `patient.Create` |
| `POST /api/patient/update-insurance` | Replace primary insurance | `patient.UpdateInsurance` |
| `POST /api/insurance/decision` | Decide plan participation for an office | `insurance.DecideInsurance` |
| `POST /api/eligibility/check` | Check payer eligibility through Stedi | `eligibility.Service.Check` |
| `POST /api/scheduler/availability` | Find policy-valid slots and sign them | `scheduling.Search` |
| `POST /api/scheduler/slots` | List openings as an inventory envelope | `scheduling.List` |
| `POST /api/appointment/book` | Revalidate and book a signed slot | `scheduling.Book` |
| `POST /api/appointment/cancel` | Verify ownership and cancel an appointment | `scheduling.Cancel` |
| `POST /api/appointment/reschedule` | Book a replacement, then cancel the confirmed original | `scheduling.Reschedule` |

Each appointment returned by patient resolution may include a private,
short-lived `cancellationToken`. A cancellation request may send that token
with the legacy patient, appointment, and office fields; supplied legacy fields
must match the signed context. A valid token cancels without appointment
rediscovery. A supplied invalid token returns `invalid_cancellation_token`
without falling back or mutating the provider. Requests without the field keep
the legacy ownership-read path during the mixed-version rollout. The token is
an agent-to-middleware value and must not enter model prompts, speech, logs, or
analytics.

The structured request log adds a PHI-free `cancellation` object for this route
with path, semantic outcome, actual provider schedule-read count, cancellation
mutation count, and duration.

Operational routes have separate contracts:

| Route | Contract |
| --- | --- |
| `GET /health`, `GET /live` | Process liveness; no provider call |
| `GET /ready` | Local initialization readiness; no provider call |
| `GET /metrics` | PHI-free patient mutation counters |
| `POST /ops/session/maintenance` | Google-signed OIDC identity; never `API_SECRET` |

Agent-readable business failures intentionally remain JSON tool results, often
with HTTP 200 and `status: "error"`. Transport authentication failures use
HTTP 401, and maintenance failures use a redacted HTTP 503.
[Provider error diagnostics](internal/clients/diagnostics.go) preserve request correlation,
provider operation/status/code, and recovered failures without exposing payloads.

## Run locally

Requirements: Go 1.26+ and valid development credentials.

```bash
cp .env.example .env
set -a
source .env
set +a

go run ./cmd/api
```

The process listens on `0.0.0.0:$PORT` (`8080` by default).

```bash
curl http://localhost:8080/live
curl http://localhost:8080/ready
```

Build and verify. `go test ./...` also runs the layer and no-comments checks.

```bash
go build ./...
go test ./...
go vet ./...
test -z "$(gofmt -l cmd internal .)"
```

The container uses the same interface:

```bash
docker build -t abita-middleware:local .
docker run --rm --env-file .env -p 8080:8080 abita-middleware:local
```

`.env` is excluded from Git and the Docker build context. Never commit
credentials or bake them into an image.

## Runtime model

Production runs one Cloud Run instance because Session and the scheduler setup
cache are process-local. Authentication correctness does not depend on
background CPU: Cloud Scheduler requests maintenance, while `Get` retains
bounded request-time recovery.

Deployment configuration and verification live in the
[deployment script](scripts/deploy-cloud-run.sh) and
[staging verification](scripts/staging_deployment.py).

## Where the details live

The README explains the system. Detailed provider and policy data stay close to
their owners:

- [Records seam](internal/advancedmd/advancedmd.go) and [adapter](internal/advancedmd/adapter.go): provider records, completeness, and write classification
- [AdvancedMD transport](internal/clients/advancedmd_xmlrpc.go): the response decoder and `oneOrMany`
- [Office policy](internal/domain/office.go): offices, scheduler columns, and routing lanes
- [Insurance decisions](internal/insurance/decision.go): participation and scheduling requirements
- [Scheduling policy](internal/scheduling/policy.go) and [tokens](internal/scheduling/tokens.go): booking rules and signed promises
- [Patient resolution](internal/patient/resolve.go): identity and appointment loading
- [Deployment](scripts/deploy-cloud-run.sh): production configuration and maintenance identity
- [Contributing](CONTRIBUTING.md): pull request, merge, and release conventions

The executable source of truth is the owning module and its interface-level tests.

## First-name/DOB resolution

`POST /api/patient/resolve` accepts `firstName` + valid `dob` without surname.
Middleware owns exact first-name/DOB matching against the complete provider
candidate set. A unique match is checked against demographics and hydrated with
insurance and appointments. No match returns `not_found`; multiple exact matches
return `multiple_matches` for staff resolution. Incomplete retrieval or conflicting
identity returns `unresolved` rather than activating a patient.

The AdvancedMD seam retrieves `lookuppatient` with `@name: ",FirstName"` and
traverses provider pagination, validating page/count evidence. The Patient module
owns identity validation. See [first-name resolution](internal/patient/first_name.go)
and its tests for the matching contract.

### Rescheduling

Rescheduling uses the existing booking and cancellation paths in one middleware
command. It needs no additional infrastructure or deployment settings. The
caller sends it once and retains the returned receipt. A replacement is booked
before the original is cancelled. `partial` and `uncertain` receipts require
reconciliation and must not be presented as completed moves. See
[rescheduling](internal/scheduling/reschedule.go) and its tests.

### Intake eligibility

`POST /api/eligibility/check` accepts the patient's `firstName`, `lastName`,
`dob`, `memberId`, `plan`, trusted `office`, and optional `coverageType`
(`medical` by default, or `routine_vision`). It requests STC `30` once per
provider without automatic retries. Eligibility is evidence, not booking
permission or proof of provider network participation.

Spring Hill medical intake checks Bach, Licht, and Noel concurrently using
their verified individual NPIs. `providerResults` retains each assessed result
with `provider: {profileId, name, firstName, lastName, npi}`. Profile IDs come
from the active scheduling registry. The top-level status and corrected identity
are usable only when all three trusted results agree; otherwise it reports
`review` / `provider_results_need_review`. Successful individual results remain
available when another request fails. The raw responses are stored only in the
individual entries, with no duplicated top-level response.

Spring Hill routine vision checks Melissa Otero, OD (NPI `1457904765`,
[CMS NPPES](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1457904765))
using the optical scheduling profile. Optical responses retain benefit rows for
STC `30` and `AL`; medical responses retain `30` and `98`. Matching rows retain
all payer qualifiers, zero amounts, and plan descriptions. Other benefit rows
are removed before the response reaches the agent or portal. Assessment uses
the original response so filtering cannot erase an error or identity conflict.

`STEDI_API_KEY` enables this path. Production deployment binds it to the
`stedi-api-key` Secret Manager secret; add a production key version and grant
the runtime service account secret access before deploying.
Crystal River uses Joseph Licht's individual NPI (`1497147680`) for eligibility,
as confirmed by the practice. Its single result uses the same `providerResults`
shape and registry profile ID as Spring Hill, so the portal can link it to
the booked physician. Sweetwater medical eligibility uses Austin Bach's individual
NPI (`1659706588`), as confirmed by the practice, with his scheduling profile in
the same single-result shape. This applies to explicit `medical` coverage and
the default when `coverageType` is omitted.

Sweetwater `routine_vision` checks Maria M. Casas (`1851438519`), Kyler Farnan
(`1568198158`), and Gisselle Calero (`1619592607`) concurrently, using the same
per-provider results and consensus rules as Spring Hill medical. Optical benefits
retain STC `30` and `AL`; partial failures retain successful provider results and
require review. Individual identities were verified against CMS NPPES on
September 24, 2026:
[Casas](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1851438519),
[Farnan](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1568198158),
[Calero](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1619592607).
The scheduling registry supplies their Sweetwater association and profile IDs;
NPPES identity verification does not establish payer enrollment or live success.

Hollywood medical eligibility uses Austin Bach (`1659706588`) and scheduling
profile `620`, matching Sweetwater medical without requiring `STEDI_PROVIDERS`.
Hollywood routine vision retains its configured provider.

North Miami Beach Optical routine vision uses Miriam Bach, OD's verified
individual NPI (`1801200977`) and its scheduling profile for appointment linkage.
See [vision payer mappings](docs/vision-eligibility-mapping.md) for supported and
unsupported Stedi routes. Other offices retain the
single-provider configuration in `STEDI_PROVIDERS`; an absent provider stays
explicitly unavailable, with no organization-NPI fallback for medical fanout.
Verified booking receipts include `profileId`, allowing the agent to select the
matching provider result for the booked appointment. Registry identity sources
(CMS NPPES, verified September 23, 2026):
[Austin Bach](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1659706588),
[Joseph Licht](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1497147680),
[Don Noel](https://npiregistry.cms.hhs.gov/api/?version=2.1&number=1659998482).
