# Insurance decisions

Middleware owns participation, canonical plan identity, attachment mapping,
referral/authorization requirements, provider restrictions, and booking enforcement.
Python collects facts, asks clarification questions, relays the backend answer, and
retains the decision for the current patient and visit type.

## Contract

`POST /api/insurance/decision` accepts `plan`, `coverageType` (`medical` or
`routine_vision`), `office`, and optional `dob`. Office uses the existing registry.
The response contains:

- `participation`: accepted, not_accepted, or unknown, scoped to `officeId` and `coverageType`.
- `canonicalPlan`, `carrierCode`: identified plan and billing code. A code is not an AMD record ID.
- `requirements`: kind, channel when relevant, and `verification: unverified`.
- `eligibility: not_checked`: participation never establishes active individual coverage.
- `allowedProviders`: intersection with the current office/age/visit scheduling policy.
- `credentialedProviders`: plan credentialing where explicitly restricted (PRE04).
- `canRegister`: the attachment mapping is usable. This does not authorize scheduling.
- `canSchedule`: participation and required backend policy are satisfied, without asserting benefits.
- `outcome`, `answer`: clarification/staff-review/acceptance instructions for the caller-facing agent.

There is no caller-writable referral or authorization verification flag. Required
referrals, authorization and network reviews remain holds: this repository has no
trusted portal verification adapter or staff-clearance record. A claimed referral,
a requirement in configuration, and an active STC 30 result cannot clear a hold.

Creation, updates and patient reads include `insuranceDecision`; legacy response
fields derive from the same decision. Writes re-evaluate the submitted plan rather
than trusting an earlier client decision. Missing carrier IDs block **before** chart
creation or end-dating existing insurance. Existing request shapes remain supported;
booking now intentionally rejects unresolved chart insurance even for raw consumers.

Patient-scoped availability accepts `patientId`, `insurancePlan`, and `coverageType`.
It reads the chart and derives routing server-side. Legacy unscoped inventory remains
available, but every booking rereads the chart and checks the selected column against
insurance policy, including signed-slot and raw-booking requests. A corrected plan
cannot silently book against a different carrier still on the chart.

Hospital follow-up bookings/reschedules collect `hospitalName` and `hospitalDate`.
Middleware checks both before booking and preserves them in appointment comments.
Neither hospital follow-up nor the referring doctor's name verifies authorization.

## Source and explicit corrections

Reference: **Abita Eye Group Insurance List - Google Sheets (1).pdf**, dated
7/7/2026, eight pages, reviewed 2026-09-17. The PDF is reference data only.
The user's explicit corrections take precedence. The office participation catalogs
previously embedded in Python moved to `internal/domain/insurance_data`; Python no
longer ships or evaluates them. The deterministic decision is in
`internal/domain/insurance_decision.go`; internal transport mappings remain in
`internal/domain/insurance.go` and consume the corrected identities.

| Plan | Code | Requirement / restriction | Attachment ID evidence |
|---|---|---|---|
| United Individual Exchange | UNI20 | PCP referral in UHC portal | Unresolved |
| United AARP Medicare Complete/Advantage HMO/LPPO | AARPM | Separate product from other United plans | Unresolved |
| United Golden Rule | GOL05 | Separate product | Unresolved |
| United Oxford | OX04 | Separate product | Unresolved |
| United Shared Services | UNIT9 | Separate product | Unresolved |
| United Student Resources | UHC STU | Separate product | Unresolved |
| United Surest | BIND1 | Separate product | Unresolved |
| United Global International | UNIT15 | VOB authorization | Unresolved |
| Preferred Care Partners | PRE04 | Medical only; Austin Bach, Calero, Casas | Existing repository carrier `car40916` |
| Humana Medicaid HMO (PDF row) | HUM02 | Authorization through Availity | Unresolved |

For unresolved IDs, a verified practice/environment carrier export is required.
Do not insert these codes into AMD's carrier-ID field or reuse a parent carrier ID.
The old Global ID is not assumed to prove the code UNIT15. Likewise, an old United
bucket does not prove UNI20 or any other corrected code. PRE04's ID is repository
crosswalk evidence, not a live provider attachment test.

The reported HUM03/HUM02 case has no verified caller plan identity or booking
outcome. The PDF identifies HUM02 as Humana Medicaid HMO, but that is not proof that
the reported caller held that product. HUM03 is unresolved. No production chart was
read or mutated to resolve the report; committed regressions use synthetic people.

## Scope conflicts and unresolved evidence

- PRE04 credentials three providers, but the current Hollywood/Sweetwater medical
  routing only enables Bach columns. Calero/Casas optical columns do not become
  medical columns through this insurance change. The contract preserves credentialed
  providers separately and only offers the intersection with enabled medical policy.
- Aetna Better Health and Molina Medicaid: the group PDF limits these to Miami-Dade,
  while older Hollywood lists include them. These return staff review, including
  vision requests, rather than accepting the older list silently.
- Freedom, Optimum, and CarePlus medical: location restrictions conflict with the
  shared Hollywood/Sweetwater catalog. Sweetwater returns staff review. CarePlus
  routine-vision credentialing is pending in the PDF.
- Aetna EPO, Imagine and MultiPlan need the underlying plan/network confirmed.
  Specific Aetna EPO network limitations and UMR authorization uncertainty remain
  unverified network-review requirements. Ambetter Value and Molina Medicare carry
  their PCP referral requirements.
- Spring Hill/Crystal River office lists are not replaced wholesale by the group
  PDF. An unlisted distinct United product cannot inherit generic United acceptance.
  Crystal River exclusions remain. Generic Humana/United/NHP names require detail.
- The migrated Hollywood `Envolve Vision` medical acceptance has no matching accepted
  backend entry. It returns a source-conflict hold.
- Other PDF billing codes have not been converted into newly guessed AMD record IDs.
  Existing mappings remain scoped by their office/coverage catalogs. This is not a
  fresh provider-directory or full benefits verification.

## Before/after evidence

Baseline middleware: `12752fa`; baseline Python: `9cc440a`.

| Synthetic scenario | Before | After / regression |
|---|---|---|
| Preferred Care Partners | Hollywood lookup and its test expected United `car40923` | PRE04 / `car40916`; `TestPreferredCareUsesItsOwnCarrierAndReceipt` checks the attachment |
| United product names | AARP, Golden Rule, Oxford, Shared Services, Student Resources and Surest collapsed to United | `TestCorrectedInsuranceIdentities` checks every distinct code and no guessed attachment |
| Humana / HUM02 | Generic Humana alias selected PPO; Medicaid used consolidated ID | Exact-plan clarification; HUM02 authorization and missing-ID hold |
| Caller supplies permissive routing | Booking reread DOB but did not validate chart insurance | `TestInsuranceRequirementsCannotBeBypassedByBookingRouting` and PRE04 column regression assert no writes |
| Hospital follow-up | No structured hospital/date fields or backend requirement | Missing-field response, persisted details, insurance holds still enforced |
| Python plan correction / late response | Synchronous local matching with a second set of rules | HTTP contract, revision guards, current-patient plan propagation, fail closed on malformed/unavailable responses |

Validation uses offline synthetic fixtures, including HTTP and native LiveKit tool
sessions. It does not prove live insurance attachment, eligibility, portal verification,
production booking, or real voice behavior.

## Integration and rollout

Patient first-name/DOB PRs middleware #188 and Python #31 are merged; their identity
contracts are unchanged. The repositories here start from main after those merges.

Eligibility PR #187 (`codex/stedi-local-matching`) was still open on review. Its
`CanonicalInsuranceName` and STC 30 service remain separate: the canonical product
names returned here preserve product identity for that service, instead of collapsing
them to carrier IDs. No second payer client, identity matcher, or eligibility evaluator
was added. `eligibility: not_checked` explicitly describes this participation endpoint;
consume #187's eligibility receipt separately. An active result must not set any
requirement's verification to verified. The unmerged service is not invoked by this
branch; combined-branch/runtime integration remains to be verified after it lands.

The separate appointment-policy/rescheduling task must retain the hooks in
`scheduling/insurance.go` and `verifyBookingPatient`, the patient/plan/coverage search
fields, and the two hospital fields. Python scheduling changes are limited to these
insurance/intake fields and response handling; appointment metadata and rescheduling
orchestration are not reimplemented here. Reconcile these small overlaps before
combining the branches.

Deploy middleware's additive decision endpoint and receipts before the Python
consumer. Python intentionally has no local fallback against old middleware. No
merge, deployment, production booking, cancellation, or chart update was performed.
