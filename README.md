# Abita Middleware

**Safe patient and scheduling workflows between Acuity's voice agent and the
clinical system of record.**

The caller asks for outcomes—find this patient, offer a valid appointment,
book this slot, cancel this visit. The middleware owns everything required to
make those outcomes safe: authentication, office and insurance policy,
eligibility, concurrency checks, provider translation, and recovery when a
write may or may not have succeeded.

It is one Go deployable, organized as a modular monolith. Provider mechanics
stay at the edge; Acuity behavior stays at the center.

## Architecture in one minute

Read this diagram from left to right. The caller speaks in patient and
scheduling intent. Deep modules turn that intent into verified outcomes.
Provider-specific endpoints, payloads, and errors live behind the records seam.

```mermaid
flowchart LR
    caller["Voice agent<br/>care intent"]
    scheduler["Cloud Scheduler<br/>session maintenance"]

    subgraph app["One Go deployable"]
        http["HTTP module<br/>authenticate · decode · map"]
        patient["Patient module<br/>Resolve · Create · UpdateInsurance"]
        scheduling["Scheduling module<br/>Search · Book · Cancel"]
        policy["Domain policy<br/>office · routing · eligibility"]
        session["Session module<br/>Get · Maintain · Status"]
        records["Records interfaces<br/>PatientRecords · SchedulingRecords"]
        adapter["Production records adapter"]

        http --> patient
        http --> scheduling
        policy --> patient
        policy --> scheduling
        patient --> records
        scheduling --> records
        records --> adapter
        session --> adapter
    end

    caller --> http
    scheduler --> http
    adapter --> provider["External system of record<br/>provider transport"]
    records -.-> testadapter["Deterministic test adapter"]
```

The architectural rule is:

```text
caller intent → owned policy → verified effect
```

The external provider is an implementation detail, not the model the rest of
the codebase is built around.

## Design from first principles

The middleware exists because the two sides of the system should not need to
understand each other:

- The voice agent should not know credentials, provider endpoints, transport
  formats, office IDs, scheduler columns, or write-recovery rules.
- The clinical system should not need to understand conversational state,
  patient intent, routing language, or how a caller should recover.
- The middleware translates between them while preserving Acuity's rules.

Four principles shape the implementation:

1. **Intent enters; provider mechanics do not leak back.** Commands and results
   use Acuity language. Raw payloads stay in the production adapter.
2. **Every rule has one owner.** Patient behavior belongs to Patient,
   scheduling behavior belongs to Scheduling, and deterministic policy belongs
   to Domain.
3. **A write is not successful until its effect is known.** Ambiguous writes
   are reconciled through authoritative reads, never blindly repeated.
4. **Interfaces are the test surface.** Production and deterministic adapters
   cross the same seam used by the workflow modules.

## The modules

Each module exposes a small interface and hides a deeper implementation. That
gives callers leverage and keeps change local.

| Module | Interface callers learn | What the implementation owns |
| --- | --- | --- |
| HTTP | Authenticated JSON routes and stable response shapes | Authentication, request IDs, transport validation, and mapping |
| Patient | `Resolve`, `Create`, `UpdateInsurance` | Identity resolution, demographics, insurance policy, patient mutations, and reconciliation |
| Scheduling | `Search`, `Book`, `Cancel` | Availability, purpose-separated signed slots and cancellations, appointment intent, live revalidation, ownership checks, and reconciliation |
| Domain | Policy and domain values | Offices, routing, eligibility, appointment types, capacity, and time rules |
| Session | `Get`, `Maintain`, `Status` | Credentials, token lifecycle, single-flight login, and last-known-good state |
| AdvancedMD | `PatientRecords`, `SchedulingRecords` | The external-records seam, production adapter, stable errors, transport, parsing, and normalization |

`cmd/api/main.go` is the composition root. It creates one Session, one
production records adapter, one Patient module, one Scheduling module, and one
HTTP router. No workflow module creates its own production dependency.

## A request through the system

```mermaid
sequenceDiagram
    autonumber
    participant C as Caller
    participant H as HTTP module
    participant W as Patient or Scheduling
    participant D as Domain policy
    participant R as Records adapter
    participant S as Session
    participant E as External system

    C->>H: Authenticated JSON intent
    H->>W: Validated command
    W->>D: Ask for deterministic policy
    D-->>W: Provider-independent decision
    W->>R: Domain read or mutation
    R->>S: Get a usable session
    S-->>R: Session data copy
    R->>E: Provider-specific request
    E-->>R: Raw response
    R-->>W: Normalized record or safe error
    W-->>H: Stable result
    H-->>C: JSON outcome + request ID
```

The HTTP module remains thin: it authenticates, validates transport shape, and
maps commands and results. It does not decide patient or scheduling policy.

## The invariants

These rules are more important than any individual endpoint:

- **One token owner.** Session is the only module that authenticates or mutates
  process-local session state.
- **One patient owner.** Patient owns complete resolution and patient mutation
  outcomes, including partial success.
- **One scheduling owner.** Scheduling owns availability, booking, and
  cancellation as one coherent workflow.
- **One policy owner.** Domain owns office, routing, DOB, provider,
  appointment-type, and capacity decisions without performing I/O.
- **Complete reads prove absence.** A missing record from a partial read is
  unknown, not absent.
- **Ambiguous writes happen once.** The implementation reconciles through a
  read or returns `indeterminate_write`; callers must not retry automatically.
- **A slot is a signed promise.** Availability signs the selected policy facts,
  and booking revalidates them against current patient and schedule state.
- **Observability is PHI-safe.** Logs contain route, status, safe category,
  latency, and a redacted request ID—not bodies, patient IDs, tokens, or raw
  provider errors.

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
- Insurance updates re-read the active demographic insurance state.
- Booking reads the intended appointment month and matches the patient, office,
  time, provider, and appointment type.
- Cancellation reads the original appointment's owning month and proves
  whether it still exists.

## The scheduling handshake

Availability and booking are deliberately one workflow. A slot can become
invalid after it is offered, so booking never trusts an old search result by
itself.

```mermaid
sequenceDiagram
    participant C as Caller
    participant S as Scheduling
    participant D as Domain policy
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

| Route | Intent |
| --- | --- |
| `POST /api/patient/resolve` | Resolve identity, demographics, routing, and upcoming appointments |
| `POST /api/add-patient` | Create a patient and attach primary insurance |
| `POST /api/patient/update-insurance` | Replace primary insurance |
| `POST /api/scheduler/availability` | Find policy-valid slots and sign them |
| `POST /api/appointment/book` | Revalidate and book a signed slot |
| `POST /api/appointment/cancel` | Verify ownership and cancel an appointment |

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

## Source map

```text
cmd/api/main.go                  composition root
internal/http/                   HTTP interface and transport mapping
internal/patient/                patient workflow
internal/scheduling/             availability, booking, and cancellation
internal/domain/                 pure policy and domain values
internal/session/                authentication and token lifecycle
internal/advancedmd/             records interfaces and production adapter
internal/advancedmd/advancedmdtest/   deterministic records adapter
internal/clients/                provider transport implementations
internal/safeerrors/             PHI-safe error classification
internal/config/                 runtime configuration
```

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

Build and verify:

```bash
go build ./...
go test ./...
go vet ./...
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

Deployment configuration, smoke checks, and rollback live in
[the Cloud Run deployment contract](docs/cloud-run-deployment.md).

## Where the details live

The README explains the system. Detailed provider and policy data stay close to
their owners:

- [AdvancedMD interface notes](docs/advancedmd-api.md) — provider operations,
  transport families, and normalization
- [Multi-office registry](MULTI_OFFICE.md) — office identifiers, scheduler
  columns, and routing lanes
- [Insurance crosswalk](INSURANCE_CROSSWALK.md) — accepted plans, carrier
  mappings, and routing outcomes
- [Patient resolution specification](docs/patient-resolve-and-appointments-spec.md)
  — identity and appointment-loading semantics
- [Cloud Run deployment contract](docs/cloud-run-deployment.md) — production
  pipeline, maintenance identity, smoke checks, and rollback
- [Release automation](docs/release-automation.md) — version and release flow
- [Contributing](CONTRIBUTING.md) — pull request and merge conventions

The executable source of truth is the owning module and its interface-level
tests. Provider reference documents explain the adapter; they do not define
workflow policy.

### Conversational appointment inventory

`POST /api/scheduler/slots` loads every eligible opening for `rangeDays` 14
(default), 30, or 90. The window counts calendar dates from the earliest
policy-permitted day. Supply the existing office, DOB, routing and
preauthorization context. The response carries coverage dates, all signed slots,
and booking-token expiry. Incomplete calendar reads return an explicit incomplete
outcome rather than presenting partial results as a complete inventory.

Reads use the existing daily appointments/block-holds adapter with at most four
concurrent days. Booking still validates the signed slot and current schedule.
Deploy this endpoint before the inventory-based agent; `/scheduler/availability`
remains available for the deployed agent and rollback. Both paths share scheduling
policy. This does not introduce pre-call fetching or a shared inventory cache.
