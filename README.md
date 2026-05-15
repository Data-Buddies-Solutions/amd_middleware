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
- Fetches appointments and block holds concurrently during availability checks.
- Returns 200 responses with `status: "error"` for agent-readable business
  errors.

## Runtime Shape

```
LiveKit agent
  -> POST /api/token
  -> POST /api/verify-patient or /api/patient-lookup
  -> POST /api/scheduler/availability
  -> POST /api/appointment/book
  -> POST /api/appointment/cancel
  -> POST /api/patient/notes

AdvancedMD middleware
  -> token manager and Redis cache
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
| `REDIS_URL` | Yes | Redis URL for token cache |
| `API_SECRET` | Yes | Bearer token required by `/api/*` endpoints |
| `PORT` | No | Server port, default `8080` |
| `AMD_ENV` | No | `dev` uses dev office IDs; anything else uses prod |

Run locally:

```bash
export ADVANCEDMD_USERNAME=...
export ADVANCEDMD_PASSWORD=...
export ADVANCEDMD_OFFICE_KEY=...
export ADVANCEDMD_APP_NAME=...
export REDIS_URL=...
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

If `office` is omitted, prod defaults to Spring Hill.

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

### POST /api/token

Conversation-init endpoint for the voice agent. Optional `office` validates and
returns the office value as a dynamic variable.

Request:

```json
{
  "office": "+19542872010"
}
```

Response:

```json
{
  "type": "conversation_initiation_client_data",
  "dynamic_variables": {
    "amd_token": "Bearer ...",
    "amd_rest_api_base": "providerapi.advancedmd.com/api/api-101/APP",
    "patient_id": "1",
    "current_date": "Thursday, May 14, 2026",
    "current_time": "3:04 PM",
    "office": "+19542872010"
  }
}
```

### POST /api/verify-patient

Looks up patients by phone plus first name, phone plus DOB, or last name plus
DOB. Returns demographics, insurance, routing, and allowed providers.

Request:

```json
{
  "phone": "9542872010",
  "dob": "01/15/1980",
  "office": "Hollywood"
}
```

Valid request shapes:

- `phone` + `firstName`
- `phone` + `dob`
- `lastName` + `dob`
- `lastName` + `dob` + `firstName`

Response statuses: `verified`, `multiple_matches`, `not_found`, `error`.

### POST /api/patient-lookup

Combined patient lookup and appointment summary. It searches by phone, optionally
filters by DOB, adds insurance routing, and returns upcoming appointments.

Request:

```json
{
  "phone": "7864657475",
  "dob": "01/15/1980",
  "office": "Sweetwater"
}
```

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
7. Existing appointments block overlapping appointment durations.
8. `maxApptsPerSlot` is respected for same-start capacity.

Response:

```json
{
  "searchedDate": "2026-05-18",
  "date": "Monday, May 18, 2026",
  "location": "ABITA EYE GROUP HOLLYWOOD",
  "providers": [
    {
      "name": "Dr. Kyler Farnan",
      "columnId": 1555,
      "profileId": 2075,
      "facility": "ABITA EYE GROUP HOLLYWOOD",
      "slotDuration": 15,
      "totalAvailable": 12,
      "firstAvailable": "8:30 AM",
      "lastAvailable": "4:15 PM",
      "slots": [
        {"time": "8:30 AM", "datetime": "2026-05-18T08:30"}
      ]
    }
  ]
}
```

### POST /api/appointment/book

Books an appointment in AdvancedMD. The middleware supplies facility ID,
appointment color, episode ID, and AMD type wrapping.

Request:

```json
{
  "patientId": "17604634",
  "patientName": "Jane Smith",
  "dob": "01/15/2019",
  "columnId": 1555,
  "profileId": 2075,
  "startDatetime": "2026-05-18T08:30",
  "duration": 15,
  "appointmentTypeId": 4245,
  "routing": "optical_only",
  "office": "Hollywood"
}
```

Required fields: `patientId`, `columnId`, `profileId`, `startDatetime`,
`duration`, `appointmentTypeId`.

Optional fields: `patientName`, `dob`, `routing`, `office`. `dob` is required
when booking an age-restricted provider column, and under-18 DOBs apply the
office's pediatric routing for medical bookings.

Booking validation:

- `patientId` must be numeric.
- `columnId` must belong to the resolved office.
- `columnId` must be valid for the requested routing lane.
- `appointmentTypeId` must be valid for the office and routing lane.
- DOB must be valid and satisfy provider age rules for age-restricted columns.
- DOB applies medical pediatric routing when the patient is under 18.
- AMD 409 conflicts return a clear slot-no-longer-available message.

Response statuses: `booked`, `error`.

### POST /api/patient/appointments

Returns upcoming appointments for a verified patient. The middleware queries
allowed columns across seven months, then filters by patient ID.

Request:

```json
{
  "patientId": "17604634",
  "office": "Sweetwater"
}
```

Response statuses: `found`, `no_appointments`, `error`.

### POST /api/appointment/cancel

Cancels an appointment by AMD appointment ID.

Request:

```json
{
  "appointmentId": 9570263
}
```

Response statuses: `cancelled`, `error`.

### POST /api/patient/notes

Saves a communication note on an existing patient. The middleware owns the AMD
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
