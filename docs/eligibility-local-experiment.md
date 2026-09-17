# Eligibility integration

`POST /api/eligibility/check` uses the existing API-secret authentication and
sends at most one Stedi request. Booking does not call it or wait for a payer.
Only STC **30** is constructed; callers cannot supply service codes, payer IDs,
provider identifiers or a free-standing eligibilitySearchId.

## Configuration and routes

Set `STEDI_API_KEY` through the approved deployment secret source and
`STEDI_PROVIDERS` to a JSON object keyed by stable office ID. Each value contains
`organizationName` and a valid `npi` for the practice. No provider is guessed from
an appointment or patient. Neither variable is required for existing middleware;
the eligibility endpoint returns 503 while unconfigured. Partial/invalid
configuration fails startup without printing its contents.

All 133 distinct insurance product labels across the office catalogs now have
an explicit payer/review disposition. See the [complete crosswalk and source evidence](eligibility-payer-mapping.md).
103 labels are executable against verified Stedi eligibility routes; 30 require
review or describe non-insurance programs. Product names and exact aliases are
used; shared AMD carrier IDs are never payer routes. Children's Medical Services
uses the service date to select Sunshine before 2026-10-01 and Molina thereafter.
Original Medicare remains blocked on enrollment/traceability; MA is not sent to CMS.

## Request and result

Synthetic initial request (dates are `YYYYMMDD`):

```json
{
  "scope": {
    "officeId": "office-id",
    "patientId": "patient-id",
    "appointmentId": "appointment-id",
    "serviceDate": "20260916"
  },
  "plan": "Oscar Health",
  "subscriber": {
    "firstName": "Jane", "lastName": "Sample",
    "dateOfBirth": "19800102", "memberId": "synthetic-member"
  }
}
```

The caller loads the confirmed patient's insurance and subscriber identity.
A dependent requires a separate `dependent` object with the actual patient's
name and DOB; the subscriber remains the policyholder. Do not infer dependent
status merely because a parent is the financial guarantor. `serviceDate` is the
booked appointment's calendar date in the office timezone, not the execution
date. Unsupported future dates remain payer rejections/unknown, never a fallback
to today's coverage. An exact returned identity is a name/DOB comparison, not
independent identity ground truth.

Results include scope, resolved `payerId` (also on blocked routes), input fingerprint, attempt number, timestamp, actual
request, Stedi check/search IDs, safe error codes, identity assessment, and
`coverage` (`active`, `inactive`, `unknown`). `status` is `completed`, `review`,
`payer_rejected`, or `unknown`. Rejection overrides active benefit rows. Only
explicit STC 30 rows determine activity; conflicting/missing activity stays
unknown. Test/unspecified response mode cannot become a production completion.
Non-exact names and ambiguous subscriber/dependent roles require review.
General plan activity does **not** prove provider participation, visit coverage,
payment, or copay. The response is evidence for the requested date, not a promise
that the payer honored that date or that coverage will remain active.

## Durable owner: external integration still required

This repository has no database, durable booking job, or eligibility result
store. Product's `backend/internal/worker/runner.go` already dispatches durable
provider commands through its `CallingWork.ClaimNextCommand` boundary; its existing messaging and
human-calling command tables belong to those domains. No eligibility command or
result owner is wired there. This endpoint is a synchronous gateway contract,
**not a durable job or idempotent HTTP endpoint**.

The remaining Product work is concrete:

1. After a confirmed booking receipt, persist an eligibility job with the office,
   patient, appointment, service date, and a frozen insurance/subscriber snapshot.
   Acquire an exclusive durable claim before every dispatch. Deduplicate the
   initial booking event and each attempt; history in the request is not a lock.
2. Journal the full input and attempt number before calling this endpoint. Retain
   its entire result securely under that patient/appointment, including terminal
   review outcomes. Do not store PHI in logs or send raw evidence to agent tools.
3. Pass all prior results in `history` for a retry of that same frozen input.
   Optional `recordedNames` contains only chart/intake first/last names with
   `source` equal to `chart` or `intake`. Middleware validates scope/fingerprint,
   carries that patient's search ID, deduplicates requests, and caps the sequence
   at four total sends. Only errors 72/73/75 permit recorded-name recovery;
   member-ID omission applies only to 72/75. No surname splitting, guessed names,
   first-name omission, DOB changes, dependent retries, or automatic discovery.
4. A timeout, process crash, lost HTTP result, unknown outcome, or stale dispatch
   claim requires reconciliation/staff review, never automatic redelivery. Keep
   the journal marked unknown until reconciled. Only explicit eligible payer
   rejections with a search ID can progress. Surface terminal review outcomes to
   staff and do not block or roll back the booked appointment.

Until that external owner exists, results are returned but **not retained by
middleware**, and automatic post-booking eligibility must remain disabled.
Deployment credentials, Product persistence/dispatch, and live provider proof
are not supplied by this implementation.

## Validation and private replay

`go test ./...` exercises synthetic provider responses, authentication, request
validation, STC 30, explicit routes, rejection precedence, unknown outcomes,
identity review, search chains, duplicate prevention within supplied history,
and the total attempt bound. The local CLI is replay-only; the obsolete paid
runner was removed so there is one outgoing eligibility path.

```
go run ./cmd/eligibility-lab -input /private/cases.json -output /private/new-results.json
```

Replay output is a new 0600 file and stdout contains aggregate counts only.
Never copy real-patient fixtures into the repository or rerun the paid cohort
for a refactor. The matcher retains exact versus fuzzy review distinctions.

Provider contract: [legacy JSON API](https://www.stedi.com/docs/healthcare/api-reference/post-healthcare-eligibility-legacy),
[submission and CMS requirements](https://www.stedi.com/docs/healthcare/send-eligibility-checks),
and [troubleshooting](https://www.stedi.com/docs/healthcare/eligibility-troubleshooting).
