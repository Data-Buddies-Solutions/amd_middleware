# AdvancedMD API Notes

This document captures the AdvancedMD surfaces that the middleware uses and how
they are wrapped by the local HTTP API. The voice agent should call the
middleware endpoints, not AdvancedMD directly.

## Authentication

AdvancedMD uses a two-step login:

1. POST to `partnerlogin.advancedmd.com`.
2. POST to the returned webserver URL.

The middleware owns this flow through `internal/session`. The Session
implementation is the only code that invokes login or mutates cached token
state. It performs single-flight request-time authentication, preserves a
still-usable last-known-good session after refresh failure, and reports
uninitialized, refreshing, fresh, stale, degraded, or unavailable status
without exposing credentials, tokens, or provider URLs.

AdvancedMD documents that the token expires 24 hours after issuance. The
Session starts proactive recovery at 20 hours and treats 24 hours as the hard
expiration boundary. Cloud Scheduler requests maintenance before the stale
threshold, and request-time fallback preserves correctness when scheduling is
delayed or fails.

Headers used downstream:

| API family | Header |
| --- | --- |
| XMLRPC | `Cookie: token=<token>` |
| REST | `Authorization: Bearer <token>` |

## Middleware Endpoints

All `/api/*` routes require `Authorization: Bearer <API_SECRET>`.

| Endpoint | Purpose |
| --- | --- |
| `GET /metrics` | PHI-free patient mutation outcome counters |
| `POST /api/patient/resolve` | Patient lookup/verification plus upcoming appointments |
| `POST /api/add-patient` | Create patient and attach insurance |
| `POST /api/patient/update-insurance` | End-date old plan and attach new plan |
| `POST /api/scheduler/availability` | Office/routing/DOB-aware availability |
| `POST /api/appointment/book` | Book appointment with server-side defaults |
| `POST /api/appointment/cancel` | Cancel appointment |

## XMLRPC APIs Used

### `lookuppatient`

Used by:

- `POST /api/patient/resolve`

Minimum request shape:

```json
{
  "ppmdmsg": {
    "@action": "lookuppatient",
    "@class": "api",
    "@name": "Smith"
  }
}
```

When first name is known, middleware sends `@name` as `LastName,FirstName` so
AMD filters common last names server-side. Phone lookups use AMD's phone lookup
path and middleware filters by DOB when DOB is supplied. First-name/DOB fallback
sends `@name` as `,FirstName`, then requires an exact first-name match and matching
DOB before resolving a unique patient.

Name and phone lookups request each `@page` through the returned `@pagecount`,
up to 100 pages. `@itemcount` is the total across pages. Changed totals, missing
or repeated patient IDs, malformed metadata, page failures, and incomplete
results return an error instead of a partial patient list. Legacy responses
without page metadata must match their item count when supplied. Sandbox reads
verified page numbering and complete traversal; production behavior remains to
be checked after deployment.

Implementation:

- `internal/patient` owns lookup selection and complete patient resolution.
- `internal/advancedmd` owns the domain-oriented provider seam and classified
  errors.
- `internal/clients/advancedmd_xmlrpc.go`
- `HandlePatientResolve` only validates and maps the Patient module result.

### `addpatient`

Used by `POST /api/add-patient`.

Middleware normalizes:

- DOB to `MM/DD/YYYY` when possible.
- Phone to AMD's expected phone format.
- Sex to `M`, `F`, or `U`.
- Names by stripping diacritical marks before sending to AMD.

The office's `DefaultProfileID` is used for the patient profile field.
`internal/patient` owns validation, normalization, office resolution, insurance
routing, mutation order, and reconciliation. `internal/advancedmd` owns the
provider payload and classifies an explicit rejection separately from an
ambiguous write. Patient captures every stable ID returned by `lookuppatient`
before creation and does not write without a complete baseline. After an
ambiguous response, only one newly appearing exact match proves success; empty,
unidentifiable, or pre-existing-only results remain `indeterminate_write`. No
creation request is retried.

### `addinsurance`

Used by:

- `POST /api/add-patient`
- `POST /api/patient/update-insurance`

The middleware maps the caller's insurance name through the medical or vision
crosswalk. `coverageType` controls the crosswalk:

- omitted or `"medical"`: medical insurance map
- `"routine_vision"`: vision insurance map

The routine-vision map is currently used for Spring Hill, Hollywood, Sweetwater,
and North Miami Beach Optical routine-vision flows. Hollywood and Sweetwater
medical requests use the 5/4/2026 Abita Eye Group list's A.Bach medical column
and route accepted medical plans to `bach_only`. North Miami Beach Optical is
routine-vision only.

An ambiguous `addinsurance` or `enddateinsurance` response is reconciled through
`getdemographic`. The active carrier, responsible party, and subscriber number
must match the intended replacement to prove it was applied; the old active
plan proves it was not. When the read cannot prove either state, the Patient
module returns `indeterminate_write` and performs no mutation retry.

### `getdemographic`

Used after patient lookup to retrieve insurance carrier, carrier ID, insurance
plan ID, and responsible party ID. The AdvancedMD adapter converts the provider
response to Acuity patient demographics before the Patient module applies
routing and preauthorization policy:

- `insuranceCarrier`
- `insuranceCarrierId`
- `insPlanId`
- `respPartyId`
- `routing`
- `allowedProviders`

### `enddateinsurance`

Used by `POST /api/patient/update-insurance` before adding the replacement
insurance plan.

### `getschedulersetup`

Used by `POST /api/scheduler/availability` to get the current scheduler columns,
profiles, and facilities.

Important fields:

| Field | Middleware use |
| --- | --- |
| `column.@id` | Scheduler `columnId` encoded into signed booking tokens and retained for legacy raw-slot booking |
| `column.@profile` | Provider `profileId` encoded into signed booking tokens and retained for legacy raw-slot booking |
| `column.@facility` | Filter columns to the resolved office's facility |
| `columnsetting.@start` | Start of provider work day |
| `columnsetting.@end` | End of provider work day |
| `columnsetting.@interval` | Slot interval in minutes |
| `columnsetting.@workweek` | Provider workdays |
| `columnsetting.@maxapptsperslot` | Same-start capacity |

The middleware does not expose every AMD column. It filters to office-owned
columns listed in `internal/domain/office.go`.

Scheduler setup is cached in process for six hours because provider columns,
profile IDs, facilities, work hours, and slot intervals are relatively static.
Actual appointments and block holds are still fetched live for each availability
search. `internal/scheduling` owns this cache, column and provider eligibility,
search completeness, slot selection, and signed-slot creation.
`internal/advancedmd` owns authentication and provider transport, returning only
domain scheduler setup and per-column schedule reads to Scheduling.

## REST APIs Used

### `GET /scheduler/appointments`

Used by:

- `POST /api/scheduler/availability`
- `POST /api/patient/resolve`

For availability, the middleware queries appointments by column and day, then
blocks candidate slots whose full duration overlaps existing appointments.
Availability responses include machine-readable outcome fields
(`outcome`, `availabilityFound`, `shouldRetrySameSearch`, and `nextAction`) so
the agent does not infer scheduling state from free-form message text. Each
returned slot includes a signed `bookingToken` that binds office, routing,
normalized DOB, provider column and profile, allowed appointment types, start
datetime, duration, same-start capacity, and force behavior for the later
booking call.
A fully exhausted search window returns `outcome: "no_availability"` with
`slots: []` and `shouldRetrySameSearch: false`. If appointment or block-hold
data is unavailable during the search and no slots are found from complete
provider reads, the middleware returns
`outcome: "availability_search_incomplete"` with `shouldRetrySameSearch: true`
instead of calling it no availability; after one retry, the agent should ask
for different preferences.

For a verified patient resolve, the middleware flattens and deduplicates the
columns for the resolved office's nearby appointment group. It issues six
multi-column monthly reads, maps every returned row to its owning office by
column ID, and filters by patient ID. Spring Hill and Crystal River are grouped
together; Hollywood and Sweetwater are grouped together. Demographics and the
six-month appointment group load concurrently after one patient is selected.
Each returned appointment includes `officeId` and `office`. Appointment loading
is best effort and reported with `appointmentsStatus` so identity resolution can
still succeed when appointment loading fails. A multiple-match lookup returns
only private lightweight candidates and performs no hydration until one
`patientId` is privately selected. Provider payloads and session data remain
inside the AdvancedMD adapter; the Patient module receives only Acuity
appointment values.

Resolved appointments include separate private signed `cancellationToken` and
`rescheduleToken` values. Both bind the patient, appointment, owning office,
start time, and appointment type, but use distinct purposes and HMAC domains.
The reschedule token can be passed privately when booking a replacement slot.
Scheduling then preserves the signed appointment type, including types outside
the canonical new-booking set. A token without a usable type falls back to
normal intent-based type resolution. Successful booking receipts include a new
private `rescheduleToken` for that appointment so a subsequent move can preserve
the same type without reloading it first.

### `GET /scheduler/blockholds`

Used by `POST /api/scheduler/availability`.

Recurring holds are interpreted as daily windows using the hold start time and
duration. The recurrence end date is not treated as the end of a same-day hold.

### `POST /scheduler/Appointments`

Used by `POST /api/appointment/book`.

The middleware builds AMD's request body from the selected availability slot
plus server-owned defaults. The preferred app-facing booking request passes
`bookingToken`; the middleware verifies the token and expands it to AMD's
office, `columnId`, `profileId`, `startDatetime`, `duration`, and routing lane
before running the same validation path. Legacy callers may still send those raw
slot fields directly only when `ALLOW_RAW_SLOT_BOOKING=true`.

- `facilityid` from the resolved office.
- `episodeid: 1`.
- `type` wrapped as `[{ "id": <appointmentTypeId> }]`.
- appointment color from `DefaultAppointmentTypeColors`; a signed reschedule of
  an unrecognized existing type uses the routine-vision fallback color.
- `force: 1` for slots whose signed `bookingToken` carries `requiresForce` from
  the preceding availability search and whose live capacity check still proves
  the same force decision.

Validation before sending to AMD:

- patient ID is numeric.
- signed booking tokens are unexpired and match `office` when `office` is
  supplied.
- column ID belongs to the office.
- column ID is valid for the requested routing lane.
- new-booking appointment type is valid for the office and routing lane.
- a reschedule type outside that set is accepted only from a valid
  `rescheduleToken` for the same patient.
- DOB is valid and satisfies provider minimum age for age-restricted columns.
- DOB applies medical pediatric routing when the patient is under 18.
- the patient demographics still match the signed DOB context.
- current scheduler setup still proves the column, profile, facility, and
  provider are eligible.
- current appointments and block holds still prove the signed capacity and
  force decision.

AMD 409 conflicts are returned as a clear slot-conflict message.
Explicit provider rejections return a stable `provider_rejected` outcome.
Ambiguous writes are never repeated automatically. Scheduling reconciles
against the patient's appointments using the intended date, time, office,
provider, and appointment type, returning a normal receipt, `write_failed`, or
`indeterminate_write`.

When the app-facing booking request includes `appointmentReason` or
`referringDoctor`, the middleware adds `comments` directly to the AMD booking
payload. The comment includes appointment reason, referring doctor, and the
`- AI` initials marker. Empty inputs are omitted entirely; a missing field in a
non-empty comment is written as `none`.

### `PUT /scheduler/appointments/{id}/cancel`

Used by `POST /api/appointment/cancel`.

The app-facing cancellation request should include `appointmentId`, `patientId`,
and `office`. Middleware reloads the patient's upcoming appointments for the
relevant nearby-office group and cancels only when the appointment belongs to
that patient. An ambiguous provider result is never repeated automatically.
Scheduling reloads current appointment state and returns a normal cancellation
receipt only when the appointment is absent, `write_failed` when it remains, or
`indeterminate_write` when the read cannot prove either outcome.

## Office Scheduler State

| Office | Facility | Medical columns | Routine-vision columns |
| --- | ---: | --- | --- |
| Spring Hill | `1568` | `1513`, `1598`, `1551`, `1550` | `1600` |
| Crystal River | `1576` | `1593` | none |
| Hollywood | `1480` | `1268`, `1478` | `1555`, `1510`, `1305` |
| Sweetwater | `670` | `682`, `1307` | `1296`, `1554`, `1210` |
| North Miami Beach Optical | `1582` | none | `1601` |

## Availability Logic

Candidate columns are filtered in this order:

1. Column belongs to the resolved office.
2. Column facility matches the resolved office facility.
3. Optional provider text matches the AMD column or profile name.
4. Routing lane filters medical vs routine-vision columns.
5. DOB applies medical pediatric routing and filters provider age rules.

Candidate slots are filtered in this order:

1. Same-day searches are rejected.
2. Preauth requests enforce a 14-day minimum lead time.
3. Provider must work that weekday.
4. Slot must be outside block holds.
5. Slot duration must not overlap a different-start existing appointment.
6. Same-start appointment count must be below per-column capacity.
7. Configured double-book columns use capacity 2 per column; partially booked
   configured slots return `sameStartBooked`, `sameStartCapacity`, and
   `requiresForce`. Hollywood and Sweetwater routine-vision columns double-book
   only for start times 8:30-10:45 AM and 1:30-2:30 PM Monday-Thursday, and
   8:30-11:45 AM on Friday. Spring Hill routine vision, North Miami Beach
   Optical, and Crystal River remain single-booked.

The response includes at most five displayed slots per provider, while
`totalAvailable` reports the full count.

## Routing And Appointment Types

Routing values:

| Routing | Columns |
| --- | --- |
| `bach_only` | Office's Bach medical columns, or Crystal River's only medical column |
| `bach_licht` | Office's Bach/Licht-capable medical lane |
| `all_three` | Office's default medical lane |
| `optical_only` | Office's routine-vision lane |
| `not_accepted` | No booking columns |

Appointment-type lane rules:

- Vision types `1010`, `3364`, `4244`, `4245` require `optical_only`.
- Medical types `1004`, `1005`, `1006`, `1007`, `1008` require a medical lane.
- Crystal River types `6167`, `6168`, `6169` are Crystal River only.

## Provider Age Rules

| Provider | Minimum age |
| --- | ---: |
| Dr. Bach | 0 |
| Dr. Calero | 4 |
| Dr. Farnan | 5 |
| Dr. Vidal | 7 |
| Dr. Casas | 7 |

The agent should pass DOB to availability, booking, and insurance-update
requests whenever it is known. Missing DOB excludes age-restricted columns from
availability and blocks booking into those columns. Under-18 DOBs apply the
office's pediatric routing for medical availability and booking.
