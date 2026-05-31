# Patient Resolve and Appointment Loading Spec

## Problem

The middleware currently has two patient identity routes with overlapping logic:

- `POST /api/verify-patient` resolves a patient from phone/name/DOB and returns demographics, insurance routing, and allowed providers.
- `POST /api/patient-lookup` resolves a patient from phone, returns the same core patient/routing data, and also loads upcoming appointments.

The agent wants the same outcome in both flows: once a patient is loaded or verified, it should usually have the patient's upcoming appointments in session state. Splitting verification from appointment loading causes extra tool calls, duplicated AdvancedMD reads, and awkward flow decisions in the agent.

## Goal

Introduce one public patient route that returns:

- Patient identity.
- Insurance/demographic routing.
- Upcoming appointments by default.
- Nearby-office appointment grouping: Spring Hill + Crystal River, and
  Hollywood + Sweetwater.
- Structured appointment-loading status so patient verification can still succeed when appointment loading fails.

The agent should use this one route for pre-call lookup, patient verification, and appointment refresh. It should not need separate middleware routes for `verify_patient`, `patient_lookup`, or `confirm_appt`.

## Non-Goals

- Do not make appointment lookup failure fail patient verification.
- Do not expose raw AdvancedMD appointment, patient, profile, or column internals beyond the current safe response shape.
- Do not require the agent to send AMD credentials, raw token data, or office internals.
- Do not keep multiple public patient lookup/appointment routes after migration.

## Proposed Endpoint

Use one canonical endpoint:

```http
POST /api/patient/resolve
```

Request:

```json
{
  "phone": "9542872010",
  "firstName": "Jane",
  "lastName": "Doe",
  "dob": "01/15/1980",
  "office": "Hollywood",
  "patientId": ""
}
```

Valid identity input shapes:

- `phone` only: pre-call lookup. Returns a single match, multiple full patient matches, or no match.
- `phone` + `firstName`: phone lookup filtered by first name.
- `phone` + `dob`: phone lookup filtered by DOB.
- `phone` + `firstName` + `dob`: phone lookup filtered by both.
- `lastName` + `dob`: name lookup filtered by DOB.
- `lastName` + `firstName` + `dob`: narrower name lookup filtered by DOB.
- `patientId`: direct load/appointment refresh for an already verified patient.

Appointment loading is always part of patient resolve. Clients should not send an appointment-loading toggle.

## Response Contract

Successful single-patient response:

```json
{
  "status": "verified",
  "patientId": "17604634",
  "name": "DOE,JANE",
  "dob": "01/15/1980",
  "phone": "(954)287-2010",
  "insuranceCarrier": "Aetna",
  "insuranceCarrierId": "123",
  "routing": "bach_only",
  "allowedProviders": ["Dr. Austin Bach"],
  "routingAmbiguous": false,
  "appointmentsStatus": "found",
  "appointments": [
    {
      "id": 9570263,
      "date": "Wednesday, March 18, 2026",
      "time": "12:00 PM",
      "provider": "Dr. Austin Bach",
      "type": "Follow Up",
      "facility": "Abita Eye Group Spring Hill",
      "officeId": "spring_hill",
      "office": "Spring Hill"
    }
  ],
  "message": "Patient verified with 1 appointment(s)"
}
```

Appointment status values:

- `found`: appointments loaded and at least one future appointment exists.
- `none`: appointments loaded and no future appointments exist.
- `error`: patient was verified, but appointment loading failed.

When appointment loading fails, keep `status: "verified"`:

```json
{
  "status": "verified",
  "patientId": "17604634",
  "name": "DOE,JANE",
  "dob": "01/15/1980",
  "phone": "(954)287-2010",
  "routing": "bach_only",
  "allowedProviders": ["Dr. Austin Bach"],
  "appointmentsStatus": "error",
  "appointments": [],
  "appointmentsMessage": "Failed to retrieve appointments",
  "message": "Patient verified, appointment lookup unavailable"
}
```

Non-single-patient statuses:

```json
{
  "status": "multiple_matches",
  "appointments": [],
  "message": "Found 2 patients for this phone number. Ask the caller to confirm their name.",
  "matches": [
    {
      "status": "verified",
      "patientId": "17604634",
      "name": "DOE,JANE",
      "dob": "01/15/1980",
      "phone": "(954)287-2010",
      "insuranceCarrier": "Aetna",
      "insuranceCarrierId": "123",
      "routing": "bach_only",
      "allowedProviders": ["Dr. Austin Bach"],
      "routingAmbiguous": false,
      "appointmentsStatus": "found",
      "appointments": [
        {
          "id": 9570263,
          "date": "Wednesday, March 18, 2026",
          "time": "12:00 PM",
          "provider": "Dr. Austin Bach",
          "type": "Follow Up",
          "facility": "Abita Eye Group Spring Hill",
          "officeId": "spring_hill",
          "office": "Spring Hill",
          "cancelToken": "signed-cancel-token"
        }
      ],
      "message": "Patient verified with 1 appointment(s)"
    },
    {
      "status": "verified",
      "patientId": "17604635",
      "name": "DOE,JOHN",
      "dob": "03/20/1982",
      "phone": "(954)287-2010",
      "insuranceCarrier": "Humana Medicare",
      "insuranceCarrierId": "456",
      "routing": "accepted",
      "allowedProviders": ["Dr. Austin Bach"],
      "routingAmbiguous": false,
      "appointmentsStatus": "none",
      "appointments": [],
      "message": "Patient verified, no appointments found"
    }
  ]
}
```

```json
{
  "status": "not_found",
  "message": "No patient found matching the provided information"
}
```

```json
{
  "status": "error",
  "message": "Failed to lookup patient by phone: ..."
}
```

## Route Semantics

`POST /api/patient/resolve`

- The only public patient lookup/load/verification/appointment-read route after migration.
- Used by pre-call phone lookup.
- Used by agent `verify_patient`.
- Used by agent `confirm_appt` when appointment state needs refresh.
- Returns appointments by default.
- Treats appointments as best effort.

Removed routes:

- `POST /api/verify-patient`
- `POST /api/patient-lookup`
- `POST /api/patient/appointments`

Those routes should not be exposed by the middleware router. The agent must move
all patient-read flows to `/api/patient/resolve`.

## Internal Design

Add shared internal resolver code instead of duplicating handler logic:

```go
type PatientResolveInput struct {
    PatientID           string
    Phone               string
    FirstName           string
    LastName            string
    DOB                 string
    Office              *domain.OfficeConfig
    IncludeAppointments bool
}

type PatientResolveResult struct {
    Status              string
    PatientID           string
    Name                string
    DOB                 string
    Phone               string
    InsuranceCarrier    string
    InsuranceCarrierID  string
    InsPlanID           string
    RespPartyID         string
    Routing             string
    AllowedProviders    []string
    RoutingAmbiguous    bool
    AppointmentsStatus  string
    Appointments        []PatientApptDetail
    AppointmentsMessage string
    Matches             []PatientResolveResult
    Message             string
}
```

Suggested helper split:

- `resolvePatientCandidates(...)`: choose phone lookup vs name lookup and return matching patients.
- `loadKnownPatient(...)`: load by patient ID for appointment refresh.
- `selectPatient(...)`: apply DOB/first-name filters and return single/multiple/not-found.
- `buildResolvedPatient(...)`: load demographics, routing, pediatric override, and response fields.
- `attachAppointments(...)`: best-effort appointment loading and appointment status.

This keeps one endpoint behavior consistent while allowing different input shapes.

## Agent Migration

1. Update pre-call `lookupByPhone` to call `/api/patient/resolve` with `phone`.
2. Update `verify_patient` to call `/api/patient/resolve` instead of `/api/verify-patient`.
3. Store returned appointments in session state for both pre-call and verification flows.
4. Update `confirm_appt` to call `/api/patient/resolve` with `patientId` only when appointment state needs refresh.
5. Change `confirm_appt` policy so it skips refresh when appointments are already present and `appointmentsStatus` is `found` or `none`.
6. If `appointmentsStatus` is `error`, allow `confirm_appt` as a retry/refresh path through the same route.
7. Preserve the first-name confirmation rule for pre-call single phone matches. Middleware `status: "verified"` means a single backend match; the agent still treats pre-call identity as matched until the caller confirms first name.

## Migration

1. Add `/api/patient/resolve`.
2. Remove `/api/patient-lookup`, `/api/verify-patient`, and `/api/patient/appointments` from the middleware router.
3. Update README and `docs/advancedmd-api.md` so `/api/patient/resolve` is the only patient read route.
4. Migrate agent pre-call, `verify_patient`, and `confirm_appt` wrappers to `/api/patient/resolve`.

## Tests

Middleware tests:

- `phone` only single match returns `status: verified`, routing, and appointments.
- `phone` only multiple matches returns full patient details and appointments for each match.
- `phone` + `firstName` resolves a multiple-match phone number and loads appointments.
- `phone` + `dob` resolves and loads appointments.
- `lastName` + `dob` resolves and loads appointments.
- `patientId` refresh loads appointments through the same route.
- appointment fetch failure still returns `status: verified` with `appointmentsStatus: error`.
- `/api/patient/resolve` always attempts appointment loading for verified patients.
- pediatric routing override still applies before allowed providers are returned.
- old patient-read routes are not registered in the router.

Agent tests:

- pre-call single match loads appointments and still requires first-name confirmation before use.
- `verify_patient` stores appointments from patient resolve.
- `confirm_appt` is skipped when appointments are already loaded.
- `confirm_appt` uses patient resolve with `patientId` when appointment status is `error`, missing, stale, or patient changed.
- multiple-match flow still asks for first name before using names from backend data.

## Open Questions

- Should the canonical route name be `/api/patient/resolve`, `/api/patient/load`, or `/api/patient/verify`?
- Should appointment loading always query six months as it does today, or should the request accept a bounded horizon?
- Should direct `patientId` load return full demographics/routing, or only patient ID plus appointments when AdvancedMD demographics are unavailable?
- Should agent deployment happen in the same release as middleware route removal, or should the middleware release be coordinated with an agent branch that is ready to deploy immediately?
