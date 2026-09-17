# Insurance decisions

Middleware owns participation, canonical plan identity, attachment mapping,
referral/authorization requirements, provider restrictions, and booking enforcement.
Python collects facts, asks clarification questions, relays the backend answer, and
retains the decision for the current patient and visit type.

## Where to change medical insurance

`internal/domain/insurance_data/MEDICAL.json` is the single medical catalog.
Each plan defines its name, aliases, billing code, verified AMD ID, clarification
question, requirements and unresolved issue once. Its `offices` entries contain
only participation status, routing and office-specific notes. Hollywood and
Sweetwater share their existing office group; explicit location restrictions still
apply in the decision function. Missing office participation requires staff review.

`insurance_medical.go` loads and validates the catalog. `insurance_matching.go`
identifies the product; `insurance_decision.go` combines it with office policy and
returns the next question or decision. There is no second medical attachment lookup.
The three duplicate medical JSON files and Go carrier/requirement tables are removed.
The existing routine-vision JSON and mappings are separate and unchanged.

When adding an alias, add it to the existing plan instead of creating another office
copy. Do not make a missing product inherit generic Medicare or another parent plan.
The generic write tests use explicitly named Meritain fixtures; product-specific
Humana tests separately verify HUM02 and authorization holds.

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
It reads the chart and derives routing server-side. Caller plan details must match
the canonical product on the chart; sharing an attachment ID is insufficient. Legacy unscoped inventory remains
available, but every booking rereads the chart and checks the selected column against
insurance policy, including signed-slot and raw-booking requests. A corrected plan
cannot silently book against a different carrier still on the chart.

Hospital follow-up bookings/reschedules collect `hospitalName` and `hospitalDate`.
Middleware checks both before booking and preserves them in appointment comments.
Neither hospital follow-up nor the referring doctor's name verifies authorization.

## Source and explicit corrections

Reference: **Abita Eye Group Insurance List - Google Sheets (1).pdf**, dated
7/7/2026, eight pages, reviewed 2026-09-17. The PDF is reference data only.
The user's explicit corrections take precedence. The 2026-09-17 follow-up
applies this document to **medical insurance only**. Routine-vision data, mappings,
and requirements are unchanged by that follow-up. Medical data previously embedded in Python is now centralized in
`internal/domain/insurance_data/MEDICAL.json`; Python does not ship or evaluate it.
The deterministic decision is in `internal/domain/insurance_decision.go`.

| Plan | Code | Requirement / restriction | Attachment ID evidence |
|---|---|---|---|
| United Individual Exchange | UNI20 | PCP referral in UHC portal | AMD directory: `car40923` |
| United AARP Medicare Complete/Advantage HMO/LPPO | AARPM | Separate product from other United plans | Unresolved |
| United Golden Rule | GOL05 | Separate product | AMD directory: `car40902` |
| United Oxford | OX04 | Separate product | AMD directory: `car284471` |
| United Shared Services | UNIT9 | Separate product | AMD directory: `car303047` |
| United Student Resources | UHC STU | Separate product | AMD directory: `car283950` |
| United Surest | BIND1 | Separate product | AMD directory: `car301501` |
| United Global International | UNIT15 | VOB authorization | Unresolved |
| Preferred Care Partners | PRE04 | Medical only; Austin Bach, Calero, Casas | AMD directory: `car40916` |
| Humana Medicaid HMO (PDF row) | HUM02 | Authorization through Availity | AMD directory: `car303033` |

The complete practice AMD directory was read on 2026-09-17: 312 unique carriers
across all seven pages. No patient charts were read or changed. Exact codes were
matched to record IDs; no billing code is written into AMD's carrier-ID field.

Additional medical corrections:

| Product | Document code | Verified attachment |
|---|---|---|
| Cigna PPO / Open Access / Miami-Dade Public Schools | CIG09 | car40895 |
| Humana Medicare PPO | HUM PPO | car303062 |
| Humana Premier HMO | HUMPHMO | car303061 |
| Molina Medicare | MOLI2 | car301507 |
| Molina Medicaid (Miami-Dade scope) | ICA01 | car40907 |
| UMR / US Health Group | UNIT3 | car284838 |
| Tricare Prime / Select | TRI00 | car284327 |
| Tricare For Life | TRI05 | car40921; Medicare-primary review |
| Aetna commercial HMO | AET07 | car40887 |
| Aetna Medicare | ICA01 | car40907 |
| Straight Florida Medicaid | FLO03 | car40899 |
| Seminole Tribe (Hollywood/Sweetwater medical) | SEMI1 | car301427; PCP referral |

Generic Medicaid requires the actual product or confirmation of straight Medicaid;
it cannot select HUM02. The reported HUM03/HUM02 case still has no verified caller
plan identity. HUM03 is the separate AMD Humana Gold record, not a catch-all for
Humana, Molina, or Cigna products.

Exact-code discrepancies remain **blocked before any chart mutation**:
AARPM versus AMD AARPMC, UNIT15 versus UNIT5, ALL1 versus ALL 1, commercial HUMPPO
versus HUM PPO, and SUNHE versus SUNHEALT. The document separately spells Medicare
PPO as HUM PPO, so that product is verified. Candidate records are not silently
substituted for disputed codes.

## Scope conflicts and unresolved evidence

- PRE04 credentials three providers, but the current Hollywood/Sweetwater medical
  routing only enables Bach columns. Calero/Casas optical columns do not become
  medical columns through this insurance change. Spring Hill's explicit PRE04
  exclusion remains; credentialing does not expand office participation.
  The contract preserves credentialed
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
- Medical eligibility/pre-certification through ehealthdeck and Envolve benefits
  review are explicit unverified scheduling holds where required by the document.
  Cigna HMO/Healthspring referrals, EMI authorization, UMR/network review, and
  Medicare-primary ordering for Tricare For Life remain holds. AvMed network and
  age-specific Engage referral details require staff review rather than a blanket
  assertion that no referral is needed.
- Missing-code arrangements such as Clear Spring, Partners Direct Health, and
  SouthBay's special self-pay rate require staff handling. Explicit AvMed Entrust,
  Jackson First HMO, Cigna Florida Connect EPO and Sure Fit exclusions are recorded.
- Carrier-directory identity is verified; individual eligibility and benefits are
  not. This is not live insurance attachment or booking proof.

## Before/after evidence

Baseline middleware: `12752fa`; baseline Python: `9cc440a`.

| Synthetic scenario | Before | After / regression |
|---|---|---|
| Preferred Care Partners | Hollywood lookup and its test expected United `car40923` | PRE04 / `car40916`; `TestPreferredCareUsesItsOwnCarrierAndReceipt` checks the attachment |
| United product names | AARP, Golden Rule, Oxford, Shared Services, Student Resources and Surest collapsed to United | `TestCorrectedInsuranceIdentities` checks every distinct code and only verified attachments |
| Humana / HUM02 | Generic Humana alias selected PPO; Medicaid used consolidated ID | Exact-plan clarification; HUM02 / car303033 with authorization hold |
| Caller supplies permissive routing | Booking reread DOB but did not validate chart insurance | `TestInsuranceRequirementsCannotBeBypassedByBookingRouting` and PRE04 column regression assert no writes |
| Hospital follow-up | No structured hospital/date fields or backend requirement | Missing-field response, persisted details, insurance holds still enforced |
| Python plan correction / late response | Synchronous local matching with a second set of rules | HTTP contract, revision guards, current-patient plan propagation, fail closed on malformed/unavailable responses |

The medical follow-up additionally exercises real patient-service update calls against
the synthetic AMD adapter, asserting exact carrier IDs in writes and preventing
mutations for disputed codes. All 1,880 routine-vision query/office results in the broader refactor comparison
were identical to the pre-change snapshot. The comparison also caught and fixed
medical parent fallbacks: named products missing office participation now require
staff confirmation instead of inheriting generic Medicare acceptance.

Validation uses offline synthetic fixtures, including HTTP and native LiveKit tool
sessions. It does not prove live insurance attachment, eligibility, portal verification,
production booking, or real voice behavior.

## Simplification review

Reviewed the first implementation (`224951b` middleware, `a91454d` Python) with
independent standards, spec and Python reviewers. The follow-up removes the second
registration/update policy calculation, the unused carrier-ID routing subsystem,
obsolete catalog control fields, and duplicated Python plan/coverage state.
Participation and attachment identity remain distinct fields in the shared catalog;
the decision function preserves unresolved conflicts as holds.

The review also found three defects in that first implementation:

- Cigna HMO -> PPO and NHP HMO Only -> Access could share a carrier ID while removing
  requirements. The regression reached a synthetic booking (`writes=1`) before the
  fix. Chart product identity now remains authoritative; both cases produce no write,
  while matching-product positive controls still book.
- Generic phrases such as "I have Humana" could choose PPO, and conflicting product
  names could be resolved by name length. Generic rules now ask for clarification;
  one matcher evaluates all known aliases and refuses conflicting products.
- PRE04 credentialing had overridden Spring Hill's office exclusion. That override
  is removed; Hollywood/Sweetwater acceptance and Spring Hill exclusion are tested.

Python additionally validates the plan/office/coverage of write decisions. Missing
write decisions cannot revive old scheduling permission; mismatched receipts remain
uncertain and cannot be retried automatically.

## Medical plan clarification

The shared medical catalog returns family-specific next questions. Humana first asks Medicare,
Medicaid, or employer/individual coverage; Humana Medicare then asks HMO/PPO and the
full product name. Cigna asks Medicare versus commercial and HMO/PPO/Open Access;
Molina asks Medicaid/Medicare/Marketplace; United asks coverage category and product,
and NHP asks Access versus Only. Aetna and Tricare require product clarification.

Broad UHC Medicare aliases no longer identify AARP, and bare Tricare no longer
identifies Select. Explicit products still resolve through their own office rules,
carrier mapping and requirements. Unknown/conflicting products cannot register or
schedule. Existing explicit office exclusions are preserved. Python relays the
backend clarification verbatim; no duplicate Python triage policy was introduced.
Routine-vision catalogs and logic are unchanged.

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
