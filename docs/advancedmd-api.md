# AdvancedMD API Notes

This document captures the AdvancedMD surfaces that the middleware uses and how
they are wrapped by the local HTTP API. The voice agent should call the
middleware endpoints, not AdvancedMD directly.

## Authentication

AdvancedMD uses a two-step login:

1. POST to `partnerlogin.advancedmd.com`.
2. POST to the returned webserver URL.

The middleware owns this flow through `internal/auth`. Tokens are cached in
Redis and refreshed by `TokenManager`.

Headers used downstream:

| API family | Header |
| --- | --- |
| XMLRPC | `Cookie: token=<token>` |
| REST | `Authorization: Bearer <token>` |

## Middleware Endpoints

All `/api/*` routes require `Authorization: Bearer <API_SECRET>`.

| Endpoint | Purpose |
| --- | --- |
| `POST /api/token` | Conversation-init data and cached AMD token |
| `POST /api/verify-patient` | Patient verification plus insurance routing |
| `POST /api/patient-lookup` | Phone lookup plus upcoming appointments |
| `POST /api/add-patient` | Create patient and attach insurance |
| `POST /api/patient/update-insurance` | End-date old plan and attach new plan |
| `POST /api/scheduler/availability` | Office/routing/DOB-aware availability |
| `POST /api/patient/appointments` | Upcoming appointments for a patient |
| `POST /api/appointment/book` | Book appointment with server-side defaults |
| `POST /api/appointment/cancel` | Cancel appointment |
| `POST /api/patient/notes` | Save patient communication note |

## XMLRPC APIs Used

### `lookuppatient`

Used by:

- `POST /api/verify-patient`
- `POST /api/patient-lookup`

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
path and middleware filters by DOB when DOB is supplied.

Implementation:

- `internal/clients/advancedmd_xmlrpc.go`
- `HandleVerifyPatient`
- `HandlePatientLookup`

### `addpatient`

Used by `POST /api/add-patient`.

Middleware normalizes:

- DOB to `MM/DD/YYYY` when possible.
- Phone to AMD's expected phone format.
- Sex to `M`, `F`, or `U`.
- Names by stripping diacritical marks before sending to AMD.

The office's `DefaultProfileID` is used for the patient profile field.

### `addinsurance`

Used by:

- `POST /api/add-patient`
- `POST /api/patient/update-insurance`

The middleware maps the caller's insurance name through the medical or vision
crosswalk. `coverageType` controls the crosswalk:

- omitted or `"medical"`: medical insurance map
- `"routine_vision"`: vision insurance map

The routine-vision map is currently used for Spring Hill, Hollywood, and
Sweetwater routine-vision flows. Hollywood and Sweetwater medical requests use
the 5/4/2026 Abita Eye Group list's A.Bach medical column and route accepted
medical plans to `bach_only`.

### `getdemographic`

Used after patient lookup to retrieve insurance carrier, carrier ID, insurance
plan ID, and responsible party ID. The response is converted to:

- `insuranceCarrier`
- `insuranceCarrierId`
- `insPlanId`
- `respPartyId`
- `routing`
- `allowedProviders`

### `enddateinsurance`

Used by `POST /api/patient/update-insurance` before adding the replacement
insurance plan.

### `savepatientnote`

Used by `POST /api/patient/notes`.

Server-owned defaults:

- `notetype`: `CN`
- `notetypefid`: `notetype559`
- `useclienttime`: `1`
- `uid`: empty
- `profilefid`: office default profile ID

Patient notes are capped at 1000 characters by the middleware.

### `getschedulersetup`

Used by `POST /api/scheduler/availability` to get the current scheduler columns,
profiles, and facilities.

Important fields:

| Field | Middleware use |
| --- | --- |
| `column.@id` | Scheduler `columnId` returned to the agent and used for booking |
| `column.@profile` | Provider `profileId` returned to the agent and used for booking |
| `column.@facility` | Filter columns to the resolved office's facility |
| `columnsetting.@start` | Start of provider work day |
| `columnsetting.@end` | End of provider work day |
| `columnsetting.@interval` | Slot interval in minutes |
| `columnsetting.@workweek` | Provider workdays |
| `columnsetting.@maxapptsperslot` | Same-start capacity |

The middleware does not expose every AMD column. It filters to office-owned
columns listed in `internal/domain/office.go`.

## REST APIs Used

### `GET /scheduler/appointments`

Used by:

- `POST /api/scheduler/availability`
- `POST /api/patient/appointments`

For availability, the middleware queries appointments by column and day, then
blocks candidate slots whose full duration overlaps existing appointments.
Availability responses include machine-readable outcome fields
(`outcome`, `availabilityFound`, `shouldRetrySameSearch`, and `nextAction`) so
the agent does not infer scheduling state from free-form message text. A fully
exhausted search window returns `outcome: "no_availability"` with `slots: []`
and `shouldRetrySameSearch: false`. If appointment data is unavailable during
the search and no slots are found from the remaining data, the middleware
returns `outcome: "availability_search_incomplete"` with
`shouldRetrySameSearch: true` instead of calling it no availability; after one
retry, the agent should ask for different preferences.

For patient appointments, the middleware queries all allowed office columns
across seven months, then filters by patient ID.

### `GET /scheduler/blockholds`

Used by `POST /api/scheduler/availability`.

Recurring holds are interpreted as daily windows using the hold start time and
duration. The recurrence end date is not treated as the end of a same-day hold.

### `POST /scheduler/Appointments`

Used by `POST /api/appointment/book`.

The middleware builds AMD's request body from the agent's selected availability
slot plus server-owned defaults:

- `facilityid` from the resolved office.
- `episodeid: 1`.
- `type` wrapped as `[{ "id": <appointmentTypeId> }]`.
- appointment color from `DefaultAppointmentTypeColors`.
- `force: 1` only for Dr. Bach slots that already have one same-start
  appointment on the selected column after a server-side re-check. Forced Bach
  bookings are post-verified and canceled if a concurrent force-book leaves the
  selected column/start over capacity.

Validation before sending to AMD:

- patient ID is numeric.
- column ID belongs to the office.
- column ID is valid for the requested routing lane.
- appointment type is valid for the office and routing lane.
- DOB is valid and satisfies provider minimum age for age-restricted columns.
- DOB applies medical pediatric routing when the patient is under 18.

AMD 409 conflicts are returned as a clear slot-conflict message.

### `DELETE /scheduler/Appointments/{id}`

Used by `POST /api/appointment/cancel`.

The middleware sends only the appointment ID. AMD rejects the older
`noshowreasonid` body for this use case, so it is intentionally omitted.

## Office Scheduler State

| Office | Facility | Medical columns | Routine-vision columns |
| --- | ---: | --- | --- |
| Spring Hill | `1568` | `1513`, `1598`, `1551`, `1550` | `1600` |
| Crystal River | `1576` | `1593` | none |
| Hollywood | `1480` | `1268`, `1478` | `1555`, `1510`, `1305` |
| Sweetwater | `670` | `682`, `1307` | `1296`, `1554`, `1210` |

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
7. Dr. Bach columns use capacity 2 per column; partially booked Bach slots
   return `sameStartBooked`, `sameStartCapacity`, and `requiresForce`.

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
