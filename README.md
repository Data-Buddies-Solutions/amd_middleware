# AdvancedMD Middleware

Production Go service that sits between LiveKit/ElevenLabs voice agents and AdvancedMD.

Its job is not just token vending. It resolves which office a call belongs to, enforces provider-routing rules, translates patient-facing insurance names into AMD carrier IDs, computes safe appointment availability, and exposes a small agent-friendly HTTP API for patient verification, registration, booking, cancellation, and insurance updates.

## What This Service Does

- Authenticates to AdvancedMD using the 2-step login flow
- Keeps a live AMD session token in memory and refreshes it every 20 hours
- Routes every request to the correct office based on the SIP trunk phone number
- Applies office-specific provider pools, pediatric rules, and insurance crosswalks
- Reads from both AMD XMLRPC and AMD REST APIs depending on the operation
- Returns agent-friendly JSON shaped for voice workflows rather than raw AMD payloads

## How The System Works

### 1. Startup

On boot, the server:

1. Initializes the office registry from `AMD_ENV`
2. Loads credentials and API auth config from environment variables
3. Builds one shared outbound HTTP client for AMD
4. Authenticates to AMD immediately
5. Stores the resulting token set in memory
6. Starts a background refresh loop that re-authenticates every 20 hours
7. Starts the HTTP server and exposes authenticated `/api/*` routes

Important: the current production code uses in-memory token storage. It does not use Redis.

### 2. Precall Token Handshake

When a new agent conversation starts, ElevenLabs calls `POST /api/token`.

That endpoint:

1. Validates the API secret
2. Resolves the office from the supplied phone number, or falls back to the default office for the active environment
3. Reads the current AMD token from the in-memory token manager
4. Returns dynamic variables for the conversation:
   - `amd_token`
   - `amd_rest_api_base`
   - `patient_id`
   - `current_date`
   - `current_time`
   - `office`

The voice agent then passes that `office` value through subsequent middleware calls.

### 3. Office Resolution

Every patient-facing operation resolves an `OfficeConfig` first.

An office config controls:

- AMD facility ID
- default profile ID for `addpatient`
- allowed provider columns
- routing tiers for insurance-based scheduling restrictions
- pediatric override behavior
- insurance mode (`medical` or `vision`)
- friendly provider-name mapping

This is what lets the same middleware support Spring Hill, Crystal River, Optical Eyeworks, and Beacon Eye without hardcoding office logic inside each handler.

### 4. Insurance And Routing

The service has two insurance lookup paths:

- medical offices use the medical crosswalk in `InsuranceNameMap`
- Optical Eyeworks and Beacon Eye use the separate vision billing crosswalk in `VisionInsuranceNameMap`

For new patients:

1. the agent sends an insurance name
2. middleware normalizes that name
3. middleware maps it to an AMD carrier ID and a routing rule
4. the response includes `routing`, `allowedProviders`, and `preauthRequired`

For existing patients:

1. middleware pulls demographics from AMD
2. middleware reads the carrier ID already on the patient
3. middleware maps the carrier ID to a routing rule
4. middleware applies pediatric override if the patient is under 18

Routing then constrains which provider columns are eligible during availability lookup.

### 5. Scheduling Flow

The scheduling path is:

1. `verify-patient` for existing patients or `add-patient` for new ones
2. use returned `routing` and `preauthRequired`
3. call `scheduler/availability`
4. present a returned slot to the caller
5. call `appointment/book`

Availability lookup is the most opinionated part of the service:

- same-day and past-date searches are rejected
- preauth cases auto-advance to a minimum of 14 days out
- provider columns are restricted by office and insurance routing
- appointments and block holds are fetched concurrently
- slot generation excludes:
  - non-working weekdays
  - block holds
  - overlapping appointments
  - same-start slots that exceed `maxApptsPerSlot`
- if the requested date is full, the service searches forward up to 14 days

### 6. Appointment Lookup / Cancel / Reschedule

Appointment confirmation and rescheduling are built on top of the same appointment lookup primitive.

`patient/appointments`:

- scans the office’s allowed columns
- fetches one month back, current month, and five months forward
- filters appointments down to the requested patient ID
- returns agent-friendly appointment objects with:
  - `id`
  - `date`
  - `time`
  - `provider`
  - `type`
  - `facility`

The middleware no longer returns a `confirmed` field in that response.

Cancellation uses the returned appointment `id` directly via AMD REST.

Rescheduling is an agent workflow, not a dedicated endpoint:

1. confirm the old appointment
2. find a new slot
3. book the new slot first
4. cancel the old appointment second

## Runtime Architecture

```text
LiveKit / ElevenLabs agent
        |
        v
POST /api/token -> dynamic vars (token, office, date/time)
        |
        v
Authenticated middleware requests (/api/*)
        |
        +--> Office registry resolution
        +--> Insurance/routing resolution
        +--> TokenManager.GetToken()
        |
        +--> AMD XMLRPC
        |     - patient lookup
        |     - demographics
        |     - add patient
        |     - add insurance
        |     - end-date insurance
        |     - scheduler setup
        |
        +--> AMD REST
              - appointments
              - block holds
              - book appointment
              - cancel appointment
```

## API Conventions

### Authentication

- `GET /health` is unauthenticated
- all `/api/*` routes require `Authorization`
- middleware accepts either:
  - `Authorization: Bearer <API_SECRET>`
  - `Authorization: <API_SECRET>`

### Status Semantics

Most application errors are returned as JSON with `status: "error"` so the agent can read the message naturally. Do not assume non-200 means failure for normal business logic.

### Request Logging

The middleware logs:

- request ID
- method/path
- request body
- response body
- duration

That is useful operationally, but it means logs may contain PHI/PII. Treat log access accordingly.

### Office Parameter

Most agent-facing endpoints accept `office`.

Expected value:

- the SIP trunk phone number in E.164 format, such as `+17275919997`

If omitted, the service falls back to the default office for the active environment.

## Current Offices

Production office registry:

| Office | Phone | Facility ID | Insurance Mode | Notes |
|---|---|---:|---|---|
| Spring Hill | `+17275919997` | `1568` | `medical` | Full routing tiers, pediatric to Bach-only |
| Optical Eyeworks | `+19542872010` | `1505` | `vision` | All routing collapses to its optical column |
| Beacon Eye | `+17864657509` | `1487` | `vision` | All routing collapses to Dr. Casas columns |
| Crystal River | `+13523202007` | `1576` | `medical` | Pediatric not accepted |

The active registry depends on `AMD_ENV`:

- `AMD_ENV=dev` loads dev office IDs and dev appointment type IDs
- anything else loads production IDs

## Request Flows

### Existing Patient Verification

`POST /api/verify-patient`

Supported inputs:

- `phone` + `firstName`
- `phone` + `dob`
- `phone` + `firstName` + `dob`
- `lastName` + `dob`

Behavior:

- strips diacritics from names before AMD lookup
- normalizes DOB format
- phone lookups search AMD by digits only
- single match returns demographics + routing
- multiple matches returns enough data for caller disambiguation

Key response fields:

- `patientId`
- `insuranceCarrier`
- `insuranceCarrierId`
- `insPlanId`
- `respPartyId`
- `routing`
- `allowedProviders`
- `routingAmbiguous`

### Combined Phone Lookup

`POST /api/patient-lookup`

Shortcut flow for phone-first experiences. It:

1. finds the patient by phone
2. optionally filters by DOB
3. attaches demographics and routing
4. best-effort attaches appointments from the current lookup window

Use this when the agent wants one call that returns both identity and appointments.

### New Patient Registration

`POST /api/add-patient`

This endpoint performs two sequential AMD XMLRPC operations:

1. `addpatient`
2. `addinsurance`

Behavior:

- patient name and DOB are normalized before submission
- phone number is normalized to AMD-friendly digits
- `ProfileID` is taken from the office config
- insurance is resolved through the office-specific insurance map
- non-accepted insurance returns `partial`, not hard failure, after patient creation

Possible statuses:

- `created`
- `partial`
- `error`

`partial` means the patient was created but insurance failed, was unrecognized, or was not accepted at that office.

### Availability Lookup

`POST /api/scheduler/availability`

Request fields:

- `date` required, `YYYY-MM-DD`
- `provider` optional
- `office` optional
- `routing` optional
- `preauthRequired` optional

Behavior:

- rejects same-day and past dates
- enforces preauth minimum lead time when requested
- fetches scheduler setup via XMLRPC
- filters setup columns to the office’s allowed columns and facility
- optionally filters by provider name
- optionally filters by routing rule
- fetches appointments and block holds concurrently for working columns
- returns up to 5 slots per provider plus `totalAvailable`
- searches forward up to 14 days if the requested date has no slots

Response shape:

- `searchedDate`
- `date`
- `location`
- `providers[]`
  - `name`
  - `columnId`
  - `profileId`
  - `facility`
  - `slotDuration`
  - `totalAvailable`
  - `firstAvailable`
  - `lastAvailable`
  - `slots[]`

### Appointment Lookup

`POST /api/patient/appointments`

Request fields:

- `patientId` required
- `office` optional

Behavior:

- reads appointments across the office’s provider columns
- searches one month back plus six months spanning current/future months
- filters to the patient ID server-side
- returns formatted appointment details for the agent

Possible statuses:

- `found`
- `no_appointments`
- `error`

### Booking

`POST /api/appointment/book`

Request fields:

- `patientId`
- `columnId`
- `profileId`
- `startDatetime`
- `duration`
- `appointmentTypeId`
- `office`

Booking protections:

- validates the column belongs to the office
- translates canonical appointment type IDs into dev IDs when `AMD_ENV=dev`
- resolves AMD booking color from the canonical type ID
- returns a caller-safe conflict message if AMD says the slot is gone

Possible statuses:

- `booked`
- `error`

### Cancellation

`POST /api/appointment/cancel`

Request fields:

- `appointmentId`

Possible statuses:

- `cancelled`
- `error`

### Insurance Update

`POST /api/patient/update-insurance`

This endpoint:

1. optionally end-dates the old plan if `insPlanId` is provided
2. attaches the new plan
3. returns the resulting routing and allowed providers

Use this after verification when the patient’s stored insurance is outdated.

## Insurance Model

### Medical Offices

Medical offices use the primary insurance crosswalk in `internal/domain/insurance.go`.

That map returns:

- AMD carrier ID
- routing tier
- preauth requirement

The routing tiers are:

- `not_accepted`
- `bach_only`
- `bach_licht`
- `all_three`

### Vision Offices

Optical Eyeworks and Beacon Eye use a separate vision billing map because many names overlap with the medical crosswalk.

Examples:

- `Humana` -> EyeMed for vision offices, not the medical Humana map
- `Florida Blue` -> Davis for vision offices
- `CarePlus` -> Alivi for vision offices

The resolver is office-aware through `LookupInsuranceForOffice(...)`.

## Project Layout

```text
cmd/
  api/                 HTTP server entrypoint
  cli/                 local AMD CLI for direct operational use

internal/auth/
  authenticator.go     AMD 2-step login
  token_manager.go     in-memory token cache + background refresh

internal/clients/
  advancedmd_xmlrpc.go AMD XMLRPC client
  advancedmd_rest.go   AMD REST client

internal/config/
  config.go            environment loading and validation

internal/domain/
  office.go            office registry, provider columns, appointment type mapping
  insurance.go         medical + vision insurance maps and routing rules
  patient.go           patient normalization helpers
  scheduler.go         availability-related domain models
  token.go             token shaping for REST/XMLRPC use

internal/http/
  router.go            route registration
  middleware.go        auth, request ID, request/response logging
  handlers.go          agent-facing HTTP handlers

internal/workspace/
  prompt and tool instructions used by the voice agent stack
```

## Configuration

Required environment variables:

| Variable | Required | Description |
|---|---|---|
| `ADVANCEDMD_USERNAME` | yes | AMD API username |
| `ADVANCEDMD_PASSWORD` | yes | AMD API password |
| `ADVANCEDMD_OFFICE_KEY` | yes | AMD office key |
| `ADVANCEDMD_APP_NAME` | yes | AMD registered app name |
| `API_SECRET` | yes | shared secret for `/api/*` routes |
| `PORT` | no | HTTP listen port, defaults to `8080` |
| `AMD_ENV` | no | `dev` for dev registry/type IDs; otherwise production |

## Local Development

### Run The API Server

```bash
export ADVANCEDMD_USERNAME=...
export ADVANCEDMD_PASSWORD=...
export ADVANCEDMD_OFFICE_KEY=...
export ADVANCEDMD_APP_NAME=...
export API_SECRET=...
export AMD_ENV=dev   # optional

go run ./cmd/api
```

### Run Tests

```bash
go test ./...
```

### Use The Local CLI

The repo also includes a local `amd` CLI under `cmd/cli` for direct operational checks such as:

- token inspection
- patient verify
- add patient
- availability lookup
- appointment lookup
- appointment cancel

## Deployment

The service is designed to run as a single long-lived process. Railway is the current deployment target.

Operational assumptions:

- one process maintains one in-memory AMD token set
- process restarts trigger a fresh AMD login during startup
- background refresh keeps the token warm
- graceful shutdown waits up to 30 seconds for the HTTP server to drain

## Important Behavioral Notes

- Same-day appointments are intentionally blocked.
- Pediatric override is office-specific.
- Unknown offices fail fast with a clear error.
- Unknown insurance names do not silently guess.
- Availability is safer by omission: if appointment data cannot be fetched for a column, that provider is skipped rather than shown as fully open.
- `patient-lookup` treats appointments as best-effort. If appointment fetch fails, identity can still be returned.
- The middleware returns agent-friendly formatted names and dates rather than raw AMD shapes.

## Source Of Truth

If this README and the code ever disagree, the code wins.

The most important files for understanding the production behavior are:

- `cmd/api/main.go`
- `internal/http/handlers.go`
- `internal/domain/office.go`
- `internal/domain/insurance.go`
- `internal/auth/token_manager.go`
- `internal/clients/advancedmd_xmlrpc.go`
- `internal/clients/advancedmd_rest.go`
