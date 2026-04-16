# AdvancedMD Token Management Service

A Go microservice that handles AdvancedMD's 2-step authentication flow and serves as the middleware layer between LiveKit conversational agents and AdvancedMD's practice management system.

## Features

- **Cached**: Tokens stored in Redis with 23-hour TTL
- **Automated**: Background goroutine refreshes tokens every 20 hours
- **Fallback**: On-demand token refresh if cache is empty
- **Reliable**: Graceful shutdown, health checks, request logging
- **Secure**: API key authentication on all endpoints
- **Concurrent**: Parallel AMD API calls for faster availability lookups

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Railway                                  │
│  ┌─────────────────┐      ┌─────────────────┐                   │
│  │  Background     │──────│     Redis       │                   │
│  │  Refresh        │      │  (token cache)  │                   │
│  │  (every 20 hrs) │      └─────────────────┘                   │
│  └─────────────────┘              │                             │
│          │                        │                             │
│  ┌───────┴─────────────────────────┴───────┐                    │
│  │              Go Gateway                  │                    │
│  │  • GET  /health              (no auth)  │                    │
│  │  • POST /api/token           (auth req) │                    │
│  │  • POST /api/verify-patient  (auth req) │                    │
│  │  • POST /api/add-patient     (auth req) │                    │
│  │  • POST /api/scheduler/availability     │                    │
│  │  • POST /api/patient/appointments      │                    │
│  │  • POST /api/appointment/book          │                    │
│  │  • POST /api/appointment/cancel        │                    │
│  └─────────────────────────────────────────┘                    │
└─────────────────────────────────────────────────────────────────┘
                              │
                      ┌───────┴───────┐
                      │  LiveKit   │
                      │  Agent        │
                      └───────────────┘
```

## Project Structure

```
advancedmd-token-management/
├── cmd/
│   └── api/
│       └── main.go              # Server entrypoint, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go            # Environment variable loading
│   ├── domain/
│   │   ├── token.go             # Token model + URL transforms
│   │   ├── patient.go           # Patient model + DOB normalization
│   │   ├── insurance.go         # Insurance routing rules + carrier maps
│   │   └── scheduler.go         # Scheduler models + availability
│   ├── auth/
│   │   ├── authenticator.go     # 2-step AdvancedMD authentication
│   │   └── token_manager.go     # Background refresh + caching
│   ├── clients/
│   │   ├── redis.go             # Pooled Redis client
│   │   ├── advancedmd_xmlrpc.go # XMLRPC client (patients, scheduler setup)
│   │   └── advancedmd_rest.go   # REST client (appointments, block holds)
│   ├── http/
│   │   ├── router.go            # chi router setup
│   │   ├── handlers.go          # Request handlers
│   │   └── middleware.go        # Auth, logging, request ID
│   └── workspace/               # Agent prompt files (git-tracked, not loaded at runtime)
│       ├── SOUL.md              # Personality + boundaries
│       ├── TOOLS.md             # API tool instructions
│       ├── VOICE.md             # Speaking style
│       ├── KNOWLEDGE.md         # Practice info (Abita Eye)
│       └── CHANGELOG.md         # Prompt change history
├── Dockerfile                   # Multi-stage build for Railway
├── go.mod
└── README.md
```

## Quick Start

### 1. Configure Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `ADVANCEDMD_USERNAME` | Your AdvancedMD API username | `DBSAPI` |
| `ADVANCEDMD_PASSWORD` | Your AdvancedMD API password | `yourpassword` |
| `ADVANCEDMD_OFFICE_KEY` | Your office key | `991NNN` |
| `ADVANCEDMD_APP_NAME` | Your registered app name | `YourAppName` |
| `REDIS_URL` | Redis connection string | `redis://default:pass@host:port` |
| `API_SECRET` | Secret for API authentication | `random-string-456` |
| `PORT` | Server port (optional, default 8080) | `8080` |

### 2. Run Locally

```bash
export ADVANCEDMD_USERNAME=...
export ADVANCEDMD_PASSWORD=...
export ADVANCEDMD_OFFICE_KEY=...
export ADVANCEDMD_APP_NAME=...
export REDIS_URL=...
export API_SECRET=...

go build -o gateway ./cmd/api && ./gateway
```

### 3. Deploy to Railway

```bash
railway login
railway up
```

## API Endpoints

### GET /health

Health check (no auth required).

```json
{"status":"ok"}
```

### POST /api/token (Precall Webhook)

LiveKit conversation initiation webhook. Returns AMD authentication tokens as dynamic variables.

**Request:**
```bash
curl -X POST -H "Authorization: Bearer YOUR_API_SECRET" \
     https://your-app.railway.app/api/token
```

**Response:**
```json
{
  "type": "conversation_initiation_client_data",
  "dynamic_variables": {
    "amd_token": "Bearer 991NNN...",
    "amd_rest_api_base": "providerapi.advancedmd.com/api/api-101/YOURAPP",
    "patient_id": "1"
  }
}
```

| Variable | Description |
|----------|-------------|
| `amd_token` | Pre-formatted Bearer token for REST API `Authorization` header |
| `amd_rest_api_base` | REST API base path (use as `https://{amd_rest_api_base}/endpoint`) |
| `patient_id` | Initial placeholder — overwritten after verify/add-patient |

### POST /api/verify-patient

Looks up a patient by first name, last name, and DOB. Names are automatically stripped of diacritical marks (e.g., "López" → "Lopez") before lookup. When `firstName` is provided, the XMLRPC `@name` parameter is sent as `"LastName,FirstName"` which lets AMD filter server-side — critical for common last names that return 1000+ paginated results.

**Request:**
```json
{
  "firstName": "John",
  "lastName": "Smith",
  "dob": "01/15/1980"
}
```

**Responses:**

| Status | When |
|--------|------|
| `verified` | Single match found — includes patientId, insurance, routing |
| `multiple_matches` | Multiple DOB matches — returns first names for disambiguation |
| `not_found` | No match |
| `error` | Auth or AMD failure |

### POST /api/add-patient

Creates a new patient and attaches insurance. Two sequential XMLRPC calls: `addpatient` then `addinsurance`.

Names are automatically stripped of diacritical marks (e.g., "López" → "Lopez") before being sent to AMD.
Insurance lookup is office-aware: medical offices use the medical insurance crosswalk, while Optical Eyeworks and Beacon Eye use a separate vision billing crosswalk.

**Request (all fields required except aptSuite):**
```json
{
  "firstName": "John",
  "lastName": "Smith",
  "dob": "01/15/1990",
  "phone": "8015551234",
  "email": "john@example.com",
  "street": "123 Main St",
  "aptSuite": "",
  "city": "Spring Hill",
  "state": "FL",
  "zip": "34609",
  "sex": "male",
  "insurance": "Humana Medicare",
  "subscriberName": "John Smith",
  "subscriberNum": "H12345678"
}
```

**Responses:**

| Status | When |
|--------|------|
| `created` | Patient + insurance both succeeded — includes routing |
| `partial` | Patient created but insurance failed/rejected |
| `error` | Validation or AMD failure |

Response includes `preauthRequired: true` when the patient's insurance requires preauthorization (Humana Gold Plus, Humana Medicaid, United Healthcare HMO, Aetna HMO, Florida Blue Medicare HMO, Cigna HMO, Tricare Prime, Tricare Forever).

### POST /api/scheduler/availability

Returns available appointment slots. Fetches appointments and block holds concurrently per column. Auto-searches forward up to 14 days if requested date is fully booked.

**Request:**
```json
{
  "date": "2026-03-03",
  "provider": "Bach",
  "office": "spring hill",
  "routing": "bach_only",
  "preauthRequired": true
}
```

Only `date` is required. `routing` comes from verify/add-patient response. When `preauthRequired` is `true`, the server enforces a 14-day minimum lead time — if the requested date is less than 14 days out, it auto-advances to the earliest allowed date.

**Response:**
```json
{
  "searchedDate": "2026-03-03",
  "date": "Monday, March 3, 2026",
  "location": "ABITA EYE GROUP SPRING HILL",
  "providers": [
    {
      "name": "Dr. Austin Bach",
      "columnId": 1513,
      "profileId": 620,
      "facility": "ABITA EYE GROUP SPRING HILL",
      "slotDuration": 30,
      "totalAvailable": 12,
      "firstAvailable": "8:00 AM",
      "lastAvailable": "4:30 PM",
      "slots": [
        {"time": "8:00 AM", "datetime": "2026-03-03T08:00"}
      ]
    }
  ]
}
```

Max 5 slots per provider. `totalAvailable` gives the full count.

#### Slot Availability Logic

A slot is available only if it passes all four checks in order:

1. **Same-day block** — If date is today (Eastern time), the request is rejected with a 400 error. Same-day appointments are not available.
2. **Block holds** — Slot is not inside any block hold (lunch, out of office, etc.)
3. **Duration overlap (AMD 4101)** — The slot's full booking range `[slotStart, slotStart+slotDuration)` must not overlap any existing appointment's range `[apptStart, apptStart+apptDuration)`. This is a bidirectional check: a 30-min booking at 8:30 is blocked by an appointment at 8:45, and a slot at 9:15 is blocked by a 30-min appointment starting at 9:00. This is a hard block — `maxApptsPerSlot` does not override it.
4. **Same-start capacity (AMD 4186)** — The number of appointments starting at this exact time is less than `maxApptsPerSlot` (0 = unlimited, skip this check)

The distinction between checks 3 and 4 matters: `maxApptsPerSlot=2` means two appointments can start at 9:00 simultaneously (double-booking), but you still cannot book at 9:15 if a 9:00 appointment's duration extends past it.

**No availability response** (when 14-day search exhausts):
```json
{
  "searchedDate": "2026-05-15",
  "date": "",
  "location": "ABITA EYE GROUP SPRING HILL",
  "message": "No availability found within 14 days of requested date",
  "providers": []
}
```

### POST /api/patient/appointments

Retrieves appointments for a verified patient. Queries all allowed provider columns across 7 months (1 past + current + 5 forward) using AMD's REST `scheduler/appointments` endpoint with `forView=month`, then filters by patient ID server-side.

**Request:**
```json
{
  "patientId": "17604634"
}
```

**Responses:**

| Status | When |
|--------|------|
| `found` | Patient has appointments in range |
| `no_appointments` | No appointments found in 7-month window |
| `error` | Validation, auth, or AMD failure |

**Response (found):**
```json
{
  "status": "found",
  "patientId": "17604634",
  "appointments": [
    {
      "date": "Thursday, March 12, 2026",
      "time": "12:00 PM",
      "provider": "Dr. Austin Bach",
      "type": "New Adult Medical",
      "facility": "Abita Eye Group Spring Hill"
    }
  ],
  "message": "Found 1 upcoming appointment(s)"
}
```

Appointment type IDs are mapped to friendly names (1006 → "New Adult Medical", etc.). Provider names are mapped to display names. Facility names are title-cased. Past appointments are filtered out.

### POST /api/appointment/book

Books an appointment in AdvancedMD. Handles appointment type → color mapping, constant fields (facilityId, episodeId), and type array wrapping server-side so the LLM only needs to pass values from the `get_availability` response.

**Request:**
```json
{
  "patientId": "17604634",
  "columnId": 1513,
  "profileId": 620,
  "startDatetime": "2026-03-20T09:00",
  "duration": 30,
  "appointmentTypeId": 1006
}
```

All fields are required. `columnId`, `profileId`, `startDatetime`, and `duration` come directly from the `get_availability` response. `appointmentTypeId` is determined by the LLM based on patient age and visit type:

| Type ID | Name | When |
|---------|------|------|
| 1006 | New Adult Medical | New patient, 18+ |
| 1004 | New Pediatric Medical | New patient, under 18 |
| 1007 | Established Adult Medical | Follow-up, 18+ |
| 1005 | Established Pediatric Medical | Follow-up, under 18 |
| 1008 | Post Op | Post-op visit, any age |

**Responses:**

| Status | When |
|--------|------|
| `booked` | Appointment created — includes appointmentId |
| `error` | Validation, auth, conflict, or AMD failure |

**Response (booked):**
```json
{
  "status": "booked",
  "appointmentId": 9570300,
  "message": "Appointment booked successfully"
}
```

**Server-side handling:**
- Maps `appointmentTypeId` → color (1006→RED, 1004→GREEN, 1007→ORANGE, 1005→PINK, 1008→BLUE)
- Sets `facilityId: 1568` (Spring Hill) and `episodeId: 1` automatically
- Wraps type as `[{id: X}]` for AMD's expected format
- Validates `columnId` against the office's allowed columns
- AMD 409 conflicts return a clear "slot no longer available" message

### POST /api/appointment/cancel

Cancels an appointment via AMD's REST API.

**Request:**
```json
{
  "appointmentId": 9570263
}
```

**Responses:**

| Status | When |
|--------|------|
| `cancelled` | Appointment successfully cancelled |
| `error` | Validation, auth, or AMD failure |

**Response (cancelled):**
```json
{
  "status": "cancelled",
  "appointmentId": 9570263,
  "message": "Appointment cancelled successfully"
}
```

The `appointmentId` comes from the `id` field in the `/api/patient/appointments` response. Error responses follow the 200-OK-with-status-error pattern used by all endpoints.

## How It Works

### Token Lifecycle

```
Startup: Load from Redis → or fresh 2-step auth → Start background refresh
Hour 20: Background refresh → 2-step AMD login → Update Redis + memory
```

### AdvancedMD 2-Step Authentication

1. POST to `partnerlogin.advancedmd.com` → Returns webserver URL
2. POST to webserver URL → Returns session token

### Insurance Routing

Medical offices use the existing medical crosswalk: 71 insurance plans consolidated to 22 carrier IDs across 4 routing tiers. `LookupInsurance()` includes an alias map for common shorthand (e.g., "Oscar" → "Oscar Health", "Humana" → "Humana PPO"). See `INSURANCE_CROSSWALK.md`.

Optical Eyeworks and Beacon Eye use a separate vision billing crosswalk through `LookupInsuranceForOffice()`. That allows overlapping names like `Humana`, `Aetna`, `Florida Blue`, `United Healthcare`, and `CarePlus` to map to the correct top-level vision billing carriers instead of the medical map.

| Routing | Providers |
|---------|-----------|
| `not_accepted` | None |
| `bach_only` | Dr. Bach |
| `bach_licht` | Dr. Bach + Dr. Licht |
| `all_three` | All 3 providers |

**Pediatric override:** Patients under 18 are automatically routed to `bach_only` (Dr. Bach is the only provider who sees pediatrics). Applied server-side in `verify-patient` and `add-patient` after insurance routing. Does not override `not_accepted`.

## Development

```bash
go test ./internal/... -v    # Run tests
go build -o gateway ./cmd/api # Build
```

## License

MIT
