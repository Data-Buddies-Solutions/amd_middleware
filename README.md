# AdvancedMD Middleware 

Go HTTP middleware between the LiveKit voice agent and AdvancedMD. It owns
AdvancedMD authentication, office routing, scheduler column selection,
insurance routing, patient lookup/creation, appointment lookup, booking,
cancellation, and patient notes.

This repo currently ships one server binary from `./cmd/api`. The old local CLI
experiment has been removed.

## Current Capabilities

- Caches AdvancedMD session tokens and refreshes them in the background.
- Exposes authenticated JSON endpoints for the voice agent.
- Resolves offices by trunk phone number, office name, or display name.
- Keeps AMD facility IDs, scheduler columns, profile IDs, and routing tiers in
  `internal/domain/office.go`.
- Uses separate medical and routine-vision insurance crosswalks.
- Filters availability and booking by office, routing lane, appointment type,
  provider column, preauth lead time, and provider age rules.
- Caches scheduler setup briefly while fetching live appointments and block
  holds for each availability search.
- Fetches appointments and block holds concurrently during availability checks.
- Returns 200 responses with `status: "error"` for agent-readable business
  errors.

## Runtime Shape

```
LiveKit agent
  -> POST /api/patient/resolve
  -> POST /api/scheduler/availability
  -> POST /api/appointment/book
  -> POST /api/appointment/cancel
  -> POST /api/patient/notes

AdvancedMD middleware
  -> in-memory token manager with background refresh
  -> AdvancedMD XMLRPC APIs for patients, demographics, notes, scheduler setup
  -> AdvancedMD REST APIs for appointments, block holds, booking, cancellation
```

## Project Structure

```
advancedmd-token-management/
|-- cmd/
|   `-- api/
|       `-- main.go
|-- docs/
|   `-- advancedmd-api.md
|-- internal/
|   |-- auth/
|   |   |-- authenticator.go
|   |   `-- token_manager.go
|   |-- clients/
|   |   |-- advancedmd_rest.go
|   |   `-- advancedmd_xmlrpc.go
|   |-- config/
|   |   `-- config.go
|   |-- domain/
|   |   |-- insurance.go
|   |   |-- office.go
|   |   |-- patient.go
|   |   |-- scheduler.go
|   |   `-- token.go
|   `-- http/
|       |-- handlers.go
|       |-- middleware.go
|       `-- router.go
|-- INSURANCE_CROSSWALK.md
|-- MULTI_OFFICE.md
|-- go.mod
`-- README.md
```

## Environment

| Variable | Required | Description |
| --- | --- | --- |
| `ADVANCEDMD_USERNAME` | Yes | AdvancedMD API username |
| `ADVANCEDMD_PASSWORD` | Yes | AdvancedMD API password |
| `ADVANCEDMD_OFFICE_KEY` | Yes | AdvancedMD office key |
| `ADVANCEDMD_APP_NAME` | Yes | Registered AdvancedMD app name |
| `API_SECRET` | Yes | Bearer token required by `/api/*` endpoints |
| `BOOKING_TOKEN_SECRET` | No | HMAC secret for signed availability slot booking tokens; defaults to `API_SECRET` |
| `ALLOW_RAW_SLOT_BOOKING` | No | Temporary legacy escape hatch for booking without `bookingToken`; default `false` |
| `ALLOW_LEGACY_CANCEL` | No | Temporary legacy escape hatch for cancelling without `cancelToken`; default `false` |
| `PORT` | No | Server port, default `8080` |
| `AMD_ENV` | No | `dev` uses dev office IDs; anything else uses prod |

Run locally:

```bash
export ADVANCEDMD_USERNAME=...
export ADVANCEDMD_PASSWORD=...
export ADVANCEDMD_OFFICE_KEY=...
export ADVANCEDMD_APP_NAME=...
export API_SECRET=...

go run ./cmd/api
```

Build:

```bash
go build -o gateway ./cmd/api
```

Test:

```bash
go test ./...
```

## Authentication

`GET /health` is unauthenticated. Every `/api/*` route requires:

```http
Authorization: Bearer <API_SECRET>
Content-Type: application/json
```

The service performs AdvancedMD's two-step login internally and caches the token.
Callers do not send AMD credentials or raw AMD session tokens.

## Office Registry

Office truth lives in `internal/domain/office.go`.

`office` request fields accept:

- E.164 trunk numbers, for example `+19542872010`
- 11-digit US numbers, for example `19542872010`
- 10-digit US numbers, for example `9542872010`
- formatted US numbers, for example `(954) 287-2010`
- office IDs or display names, for example `hollywood`, `Hollywood`,
  `sweetwater`, `Spring Hill`

If `office` is omitted on non-token requests, prod defaults to Spring Hill.
Signed `bookingToken` and `cancelToken` requests infer office from the token and
reject a conflicting explicit office.

### Production Offices

| Office | ID | Facility | Default Profile | Phone mappings |
| --- | --- | ---: | ---: | --- |
| Spring Hill | `spring_hill` | `1568` | `620` | `+17275919997` |
| Crystal River | `crystal_river` | `1576` | `2064` | `+13523202007`, `+16182265883` placeholder |
| Hollywood | `hollywood` | `1480` | `620` | `+19542872010` |
| Sweetwater | `sweetwater` | `670` | `620` | `+17864657475`, `+17864654845`, `+17866134310`, `+17864657479`, `+17864654836`, `+17864654882` |

### Scheduler Columns

| Office | Lane | Columns |
| --- | --- | --- |
| Spring Hill | Medical | `1513` Dr. Bach, `1598` Dr. Bach Overflow, `1551` Dr. Licht, `1550` Dr. Noel |
| Spring Hill | Routine vision | `1600` Routine Vision Dr. Melissa Otero |
| Crystal River | Medical | `1593` Dr. Licht |
| Hollywood | Medical | `1268` Dr. Bach, `1478` Dr. Bach HW Overflow |
| Hollywood | Routine vision | `1555` Dr. Farnan, `1510` Dr. Vidal, `1305` Dr. Calero |
| Sweetwater | Medical | `682` Dr. Bach, `1307` Dr. Bach Overflow |
| Sweetwater | Routine vision | `1296` Dr. Casas, `1554` Dr. Farnan, `1210` Dr. Calero |

### Provider Age Rules

Medical pediatric routing still sends minors to the office's pediatric routing
lane. Provider-specific age limits are enforced for availability and booking:
missing DOB excludes age-restricted providers from availability and blocks
booking into those columns; malformed DOB is rejected.

| Provider | Minimum age |
| --- | ---: |
| Dr. Bach | Newborn and up |
| Dr. Calero | 4 and up |
| Dr. Farnan | 5 and up |
| Dr. Vidal | 7 and up |
| Dr. Casas | 7 and up |

## Routing And Insurance

Routing values:

| Routing | Meaning |
| --- | --- |
| `not_accepted` | Insurance is not accepted |
| `bach_only` | Medical lane for Dr. Bach columns |
| `bach_licht` | Medical lane for offices that have Bach and Licht |
| `all_three` | Full medical lane for the office |
| `optical_only` | Routine-vision lane |

Current state:

- Spring Hill and Crystal River use the existing medical insurance map.
- Hollywood and Sweetwater use the Abita Eye Group 5/4/2026 medical insurance
  list's A.Bach column. Accepted medical plans route to `bach_only` and map to
  the existing AMD network carrier IDs.
- Routine vision for Spring Hill, Hollywood, and Sweetwater uses the existing
  vision insurance crosswalk and returns `routing: "optical_only"`.
- `coverageType` defaults to medical. Send `"routine_vision"` only after the
  agent has confirmed an accepted vision plan.
- Medical patients under 18 are routed through the office's pediatric routing
  unless the insurance is not accepted. When DOB is present, availability and
  booking also apply that pediatric routing even if the caller accidentally
  sends a broader medical routing lane.

## Appointment Types

| Type ID | Name | Lane |
| ---: | --- | --- |
| `1006` | New Adult Medical | Medical |
| `1004` | New Pediatric Medical | Medical |
| `1007` | Established Adult Medical | Medical |
| `1005` | Established Pediatric Medical | Medical |
| `1008` | Post Op | Medical |
| `1010` | New Adult Vision | Routine vision |
| `3364` | Established Adult Vision | Routine vision |
| `4244` | New Pediatric Vision | Routine vision |
| `4245` | Established Pediatric Vision | Routine vision |
| `6167` | Crystal River New Patient | Crystal River only |
| `6168` | Crystal River Post Op | Crystal River only |
| `6169` | Crystal River Established Patient | Crystal River only |

The booking endpoint rejects medical types on `optical_only`, vision types on
medical routing, and Crystal River-specific types outside Crystal River.

## Endpoints

### GET /health

Returns:

```json
{"status":"ok"}
```

### POST /api/patient/resolve

Single patient read route for pre-call lookup, patient verification, and
appointment refresh. It resolves patients by phone, name/DOB, or known
`patientId`; returns demographics, insurance routing, allowed providers, and
loads upcoming appointments by default.

Request:

```json
{
  "phone": "9542872010",
  "firstName": "Jane",
  "dob": "01/15/1980",
  "office": "Hollywood",
  "includeAppointments": true
}
```

Valid request shapes:

- `phone`
- `phone` + `firstName`
- `phone` + `dob`
- `phone` + `firstName` + `dob`
- `lastName` + `dob`
- `lastName` + `dob` + `firstName`
- `patientId`

Appointment loading is best effort. A verified patient response uses
`appointmentsStatus` to separate identity resolution from appointment loading:
`found`, `none`, `skipped`, or `error`. Appointment responses include
`cancelToken`, a signed short-lived token binding the appointment to the patient
and office. The agent should store this token in tool/session state and pass it
back when cancelling.

Response statuses: `verified`, `multiple_matches`, `not_found`, `error`.

### POST /api/add-patient

Creates the patient with XMLRPC `addpatient`, then attaches insurance with
`addinsurance`.

Request:

```json
{
  "firstName": "John",
  "lastName": "Smith",
  "dob": "01/15/1990",
  "phone": "8015551234",
  "email": "john@example.com",
  "street": "123 Main St",
  "aptSuite": "",
  "city": "Hollywood",
  "state": "FL",
  "zip": "33021",
  "sex": "male",
  "insurance": "Humana Medicare",
  "coverageType": "medical",
  "subscriberName": "John Smith",
  "subscriberNum": "H12345678",
  "office": "Hollywood"
}
```

Required fields: `firstName`, `lastName`, `dob`, `phone`, `street`, `city`,
`state`, `zip`, `insurance`, `subscriberName`, `subscriberNum`.

Optional fields: `email`, `aptSuite`, `coverageType`, `office`.

Response statuses: `created`, `partial`, `error`.

### POST /api/patient/update-insurance

End-dates the old insurance plan and attaches a new one.

Request:

```json
{
  "patientId": "17604634",
  "dob": "01/15/1980",
  "insPlanId": "12345",
  "respPartyId": "67890",
  "oldInsurance": "Old Carrier",
  "insurance": "Humana Medicare",
  "coverageType": "medical",
  "subscriberName": "John Smith",
  "subscriberNum": "H12345678",
  "office": "Spring Hill"
}
```

`dob` is optional, but it should be supplied when known so the response can
return age-filtered `allowedProviders` and apply medical pediatric routing.

Response statuses: `updated`, `error`.

### POST /api/scheduler/availability

Returns availability from AMD scheduler setup, appointments, and block holds.
Searches forward up to 14 days when the requested date has no slots.

Request:

```json
{
  "date": "2026-05-18",
  "provider": "Farnan",
  "office": "Hollywood",
  "routing": "optical_only",
  "dob": "01/15/2019",
  "preauthRequired": false
}
```

Only `date` is required. `routing` defaults to `all_three`, which means medical
columns only. Routine vision must send `routing: "optical_only"`. `dob` is
optional for unrestricted columns; age-restricted provider columns are excluded
when DOB is missing, malformed DOB returns an error, and under-18 DOBs apply the
office's pediatric routing for medical availability.

Availability rules:

1. Same-day appointment searches are rejected.
2. `preauthRequired: true` enforces a 14-day minimum lead time.
3. Columns must belong to the resolved office and facility.
4. Routing controls which medical or routine-vision columns are considered.
5. DOB applies medical pediatric routing and filters provider age rules.
6. Recurring block holds use the daily hold window, not the recurrence end date.
7. Different-start appointments block overlapping appointment durations.
8. Same-start appointment count is checked against per-column capacity.
9. Dr. Bach columns allow one existing same-start appointment per column; those
   slots include `sameStartBooked`, `sameStartCapacity`, and `requiresForce`.

Response:

```json
{
  "status": "success",
  "outcome": "availability_found",
  "availabilityFound": true,
  "requestedDate": "2026-05-18",
  "shouldRetrySameSearch": false,
  "nextAction": "offer_slots",
  "actualDate": "2026-05-18",
  "slots": [
    {
      "provider": "Dr. Kyler Farnan",
      "time": "8:30 AM",
      "datetime": "2026-05-18T08:30",
      "bookingToken": "signed-slot-token",
      "columnId": 1555,
      "profileId": 2075,
      "duration": 15
    }
  ]
}
```

When no slots exist in the full search window, the endpoint returns a completed
tool result, not an execution error. The agent should treat
`outcome: "no_availability"` and `shouldRetrySameSearch: false`
as the control fields:

```json
{
  "status": "success",
  "outcome": "no_availability",
  "availabilityFound": false,
  "requestedDate": "2026-05-18",
  "shouldRetrySameSearch": false,
  "nextAction": "ask_for_different_preferences",
  "searchedFrom": "2026-05-18",
  "searchedThrough": "2026-06-01",
  "message": "No availability was found from 2026-05-18 through 2026-06-01. Do not search this same window again unless the patient changes date, provider, office, or appointment type.",
  "slots": []
}
```

If AMD appointment data is unavailable for any searched provider/date and no
slots are found from the remaining data, the response is
`outcome: "availability_search_incomplete"` with `shouldRetrySameSearch: true`.
The agent should retry once; if it still cannot check availability, it should
ask for different preferences.

### POST /api/appointment/book

Books an appointment in AdvancedMD. The preferred path is to pass the signed
`bookingToken` from the selected availability slot. The middleware expands that
token into the raw AMD slot identifiers, then supplies facility ID, appointment
color, episode ID, AMD type wrapping, and the numeric AMD appointment type.

Request:

```json
{
  "patientId": "17604634",
  "patientName": "Jane Smith",
  "dob": "01/15/2019",
  "bookingToken": "signed-slot-token",
  "visitCategory": "routine_vision",
  "visitKind": "routine_vision",
  "patientStatus": "established",
  "office": "Hollywood"
}
```

Legacy raw-slot request, disabled by default and intended only as a temporary
compatibility escape hatch with `ALLOW_RAW_SLOT_BOOKING=true`. Legacy callers
may still send `appointmentTypeId`; otherwise the same intent fields are used:

```json
{
  "patientId": "17604634",
  "patientName": "Jane Smith",
  "dob": "01/15/2019",
  "columnId": 1555,
  "profileId": 2075,
  "startDatetime": "2026-05-18T08:30",
  "duration": 15,
  "visitCategory": "routine_vision",
  "visitKind": "routine_vision",
  "patientStatus": "established",
  "appointmentReason": "blurry vision",
  "referringDoctor": "Dr. Smith",
  "routing": "optical_only",
  "office": "Hollywood"
}
```

Required fields: `patientId`, appointment intent (`visitCategory`/`visitKind`,
`patientStatus`, and `dob` or `ageBand` when age matters), and either
`bookingToken` or the legacy raw slot fields `columnId`, `profileId`,
`startDatetime`, and `duration`. `appointmentTypeId` is accepted only as a
legacy override; new callers should not send it.

Optional fields: `patientName`, `dob`, `ageBand`, `routing`, `office`,
`isPostOp`, `visitReason`, `appointmentReason`, and `referringDoctor`. When
`appointmentReason` or `referringDoctor` is present, booking first creates the
appointment, then saves an AP patient note with the new appointment ID in the
note body. `dob` is required when booking an age-restricted provider column,
and under-18 DOBs apply the office's pediatric routing for medical bookings.
When `bookingToken` is used, the token owns the office, selected `columnId`,
`profileId`, `startDatetime`, `duration`, and routing lane.

Booking validation:

- `patientId` must be numeric.
- `bookingToken`, when present, must be signed and unexpired. If `office` is
  supplied, it must match the token's office.
- `columnId` must belong to the resolved office.
- `columnId` must be valid for the requested routing lane.
- Resolved appointment type must be valid for the office and routing lane.
- If the appointment type cannot be resolved, the response uses
  `outcome: "appointment_type_unresolved"` with `missing` facts such as
  `patientStatus`, `dob`, or `routeToSpringHill`.
- DOB must be valid and satisfy provider age rules for age-restricted columns.
- DOB applies medical pediatric routing when the patient is under 18.
- Bach slots that availability marked `requiresForce` are booked with AMD
  `force: 1` from the signed `bookingToken`; booking does not re-fetch
  appointments or block holds for Bach. If AMD reports a conflict, the response
  asks the caller to choose another slot.
- AMD 409 conflicts return a clear slot-no-longer-available message.

Response statuses: `booked`, `partial`, `error`. `partial` means the
appointment was booked but the follow-up AP note failed.

### POST /api/appointment/cancel

Cancels an appointment with a middleware-issued cancel token. Legacy
appointment-ID-only cancellation is disabled unless `ALLOW_LEGACY_CANCEL=true`.

Request:

```json
{
  "appointmentId": 9570263,
  "patientId": "17604634",
  "cancelToken": "signed-cancel-token",
  "office": "Sweetwater"
}
```

Response statuses: `cancelled`, `error`.

### LiveKit Agent Contract

The LiveKit agent no longer needs `/api/token`; that endpoint has been removed.
The agent should call middleware endpoints directly with `AMD_API_URL` and
`AMD_API_TOKEN`.

For cancellation, `confirm_appt` / appointment lookup should keep each
appointment's hidden `cancelToken` in session state. `cancel_appt` should send:

```json
{
  "appointmentId": 9570263,
  "patientId": "17604634",
  "cancelToken": "signed-cancel-token"
}
```

### POST /api/patient/notes

Saves an appointment note on an existing patient. The middleware owns the AMD
note type and default profile ID.

Request:

```json
{
  "patientId": "17604634",
  "note": "Patient called to reschedule. Appointment updated.",
  "office": "Hollywood"
}
```

Notes are capped at 1000 characters.

Response statuses: `saved`, `error`.

## Development Notes

- Keep office data in `internal/domain/office.go`.
- Keep medical and vision insurance logic in `internal/domain/insurance.go`.
- Prefer endpoint tests in `internal/http/handlers_test.go` for user-visible
  behavior.
- Prefer domain tests in `internal/domain/*_test.go` for office, insurance, DOB,
  and appointment-type rules.

## License

MIT
