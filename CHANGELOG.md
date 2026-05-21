# Changelog

## [Unreleased] - 2026-05-21

### Slot Booking Tokens

- Added signed `bookingToken` values to availability slots so callers can book
  the selected slot without exposing raw AMD `columnId`, `profileId`,
  `startDatetime`, or `duration` to the model.
- Extended appointment booking to accept `bookingToken`, verify the token's
  signature, expiry, and office, then expand the slot details server-side before
  the existing booking validations.
- Added `BOOKING_TOKEN_SECRET` with fallback to `API_SECRET` for deployments
  that do not need a separate slot-token secret yet.

### Bach Same-Start Double Booking

- Added per-column Dr. Bach same-start capacity of 2. Availability now keeps a
  Bach slot visible when exactly one appointment already starts at that time,
  while still blocking different-start duration overlaps and block holds.
- Added `sameStartBooked`, `sameStartCapacity`, and `requiresForce` metadata on
  partially booked availability slots.
- Added server-owned AMD `force: 1` booking support for Bach slots after
  re-checking the selected column's appointments and block holds.
- Bach booking now serializes each office/column/start within a process and
  post-verifies forced bookings, canceling the new appointment if capacity was
  exceeded by a concurrent force-book.

## [Unreleased] - 2026-05-14

### Hollywood and Sweetwater Scheduler Support

- Added a compact agent-facing availability response for
  `/api/scheduler/availability`: `status`, `outcome`, `availabilityFound`,
  `shouldRetrySameSearch`, `nextAction`, and flat bookable `slots`.
- The exhausted 14-day no-availability result now returns
  `outcome: "no_availability"`, `searchedFrom`, `searchedThrough`, and
  no-retry guidance, so the agent should stop re-calling `get_availability` for
  the same window.
- If appointment data is unavailable during the search, the endpoint now returns
  `outcome: "availability_search_incomplete"` and `shouldRetrySameSearch: true`;
  after one retry, the agent should ask for different preferences instead of
  treating the window as exhausted no-availability.
- Added Hollywood and Sweetwater office configs, phone mappings, scheduler
  columns, routine-vision lanes, and provider age rules.
- Added Hollywood and Sweetwater medical insurance routing from the 5/4/2026
  Abita list's A.Bach column, mapping accepted plans to existing network carrier
  IDs and routing them through `bach_only`.
- Added office lookup support for E.164, 11-digit US, 10-digit US, formatted
  phone numbers, office IDs, and office display names.
- Extended routine-vision appointment type validation to any office with an
  `optical_only` routing lane.
- Added DOB-aware provider filtering for availability and booking.
- Removed the old local CLI experiment and its Cobra dependencies; the repo now
  ships only the HTTP API binary from `cmd/api`.
- Bumped Go module dependencies `golang.org/x/net` to `v0.54.0` and
  `golang.org/x/text` to `v0.37.0`.
- Rewrote README, multi-office docs, and AdvancedMD API notes to match the
  current middleware state.

## [Unreleased] - 2026-03-23

### Availability Audit — Bug Fixes, Performance, and Patient Lookup

Comprehensive audit of the availability endpoint uncovered multiple bugs causing booked slots to appear as available in production. Also added parallel fetching, working-day skip optimization, phone-based patient lookup, and a new combined patient-lookup endpoint.

#### Fixed

- **Ghost entries dropping real appointments** — `GetAppointments`/`GetBlockHolds` used pre-allocated slices with `continue` on parse errors, leaving zero-value entries that silently replaced real appointments. Now uses `append` so skipped entries don't leave ghosts.
- **All-or-nothing error propagation** — If any single column's appointment fetch failed, ALL columns' data was discarded and the handler proceeded with empty data (showing every slot as open). Now per-column errors are isolated — failed columns are logged and omitted; successful columns are preserved.
- **14-day search window shrinking on non-working days** — The `attempt` counter incremented even when days were skipped (weekends, non-working days), reducing the effective search window. Now uses calendar date comparison (`maxDate = startDate + 14 days`) so skipped days don't consume the budget.
- **Past dates not rejected** — Same-day was blocked but past dates (e.g., 3 days ago) passed through and returned stale "available" slots. Now rejects any date <= today.
- **HandleGetPatientAppointments inline datetime parsing** — Used only 2 formats, inconsistent with the 5-format `ParseDateTime`. Now calls the shared helper.

#### Added

- **`ParseDateTime` helper** (`clients/advancedmd_rest.go`) — Robust datetime parser trying 5 formats (ISO 8601, RFC3339, RFC3339Nano, fractional seconds). Strips timezone for consistent wall-clock comparisons. Exported and used by all datetime parsing paths.
- **Single-object JSON fallback** — `GetAppointments`/`GetBlockHolds` now handle AMD's single-vs-array response quirk (tries `[{...}]` then `{...}`), matching the XMLRPC client's existing pattern.
- **`LookupPatientByPhone`** (`clients/advancedmd_xmlrpc.go`) — New XMLRPC method using AMD's `@phone` parameter. Extracted `parseLookupResponse` as shared helper for both name and phone lookups.
- **`POST /api/patient-lookup`** — Combined endpoint: phone → patient identity + insurance routing + upcoming appointments in one call. Designed for precall/early-call agent use.
- **Phone support on `POST /api/verify-patient`** — Accepts optional `phone` field as alternative to `lastName`.
- **`StripToDigits` exported** (`domain/office.go`) — Previously unexported, now available to handlers.

#### Improved

- **Parallel appointment + block hold fetching** — Independent data fetched concurrently per day searched (~2x faster per day).
- **Working-day skip** — Before making API calls, checks if any allowed column works that weekday. Skips non-working days entirely (zero API calls). For Dr. Licht (Wed-Thu only), a typical search saves 5 out of 8 days of API calls.
- **Dead error return removed** — `GetAppointmentsForColumns`/`GetBlockHoldsForColumns` now return just the map (error was always nil after per-column isolation).
- **`HandleGetPatientAppointments` deduplicated** — Was 100+ lines of copy-paste from `fetchUpcomingAppointments`. Now calls the shared helper.

#### Changed

- **`CLAUDE.md`** — Dr. Bach `maxApptsPerSlot` corrected from `0 (unlimited)` to `1` (confirmed via AMD 4186 error in production call).

---

## [Unreleased] - 2026-03-13

### Reschedule Flow + Cancel Fix

Agent can now handle rescheduling directly instead of transferring. Uses existing tools in sequence: verify → confirm_appt → get_availability → book_appt → cancel_appt. Books new appointment before cancelling old one to protect the patient.

#### Changed

- **`internal/clients/advancedmd_rest.go`** — Removed `noshowreasonid` from cancel request body. AMD returns HTTP 500 when it's included; works with just `{"id": appointmentID}`. Removed `cancelNoshowReasonID` constant.
- **`internal/clients/advancedmd_rest_test.go`** — Removed `noshowreasonid` assertion from `TestCancelAppointment_Success`
- **`internal/workspace/TOOLS.md`** — Added "Rescheduling" section (book-first-then-cancel flow); updated intent routing (reschedule no longer transfers); updated transfer_to_number (removed reschedule from transfer reasons)
- **`internal/workspace/SOUL.md`** — "Stay in your lane" updated to include rescheduling as a capability
- **`README.md`** — Removed "hardcoded no-show reason ID (23)" from cancel endpoint docs

---

## [Unreleased] - 2026-03-10

### Preauthorization — 14-Day Minimum Lead Time for HMO/Managed Care Plans

8 insurance plans require preauthorization before scheduling. The `add-patient` response now includes `preauthRequired: true` for these plans, and the availability endpoint enforces a 14-day minimum — if the requested date is too soon, it auto-advances to 14 days out.

#### Added

- **`internal/domain/insurance.go`** — `PreauthRequired bool` field on `InsuranceEntry`. Flagged on: Humana Gold Plus, Humana Medicaid, United Healthcare HMO, Aetna HMO, Florida Blue Medicare HMO, Cigna HMO, Tricare Prime, Tricare Forever
- **`internal/http/handlers.go`** — `preauthRequired` field on `AddPatientResponse`; `preauthRequired` param on `AvailabilityRequest`; `enforcePreauthMinDate()` function that auto-advances dates < 14 days out
- **`internal/http/handlers_test.go`** — `TestEnforcePreauthMinDate` with 5 cases (tomorrow, 7d, 13d, exactly 14d, 30d)
- **`internal/workspace/TOOLS.md`** — Agent prompt: preauth insurance list, patient explanation script, `preauthRequired` param on `get_availability`

### Insurance Updates — New Plans + Renamed Humana Gold

#### Added

- **`internal/domain/insurance.go`** — Aetna HMO, United Healthcare HMO, Florida Blue Medicare HMO, Tricare Forever, BCBS Medicare HMO alias
- **`INSURANCE_CROSSWALK.md`** — Updated tables and plan counts

#### Changed

- **`internal/domain/insurance.go`** — Renamed `humana gold` → `humana gold plus`

---

### Patient Lookup — Send FirstName to AMD for Accurate Matching

Patients with common last names (e.g., "Gonzalez" — 1042 results across 21 pages) were returning `not_found` because the middleware only read page 1 of AMD's paginated `lookuppatient` response. The fix sends `"LastName,FirstName"` in the XMLRPC `@name` field, letting AMD filter server-side and return exact matches instead of thousands of paginated results.

#### Changed

- **`internal/clients/advancedmd_xmlrpc.go`** — `LookupPatient` now accepts `firstName` parameter. When provided, sends `@name` as `"LastName,FirstName"` instead of just `"LastName"`
- **`internal/http/handlers.go`** — `verify-patient` handler now normalizes and passes `firstName` through to `LookupPatient`
- **`internal/workspace/TOOLS.md`** — Agent prompt updated: first name is now required for `verify_patient`, agent asks caller to spell both first and last name
- **`README.md`** — Updated `verify-patient` docs to reflect firstName importance and AMD filtering behavior

---

## [Unreleased] - 2026-03-05

### Availability Slot Calculation — Separate AMD 4101 / 4186 Conflict Checks

Agent tried to book Dr. Licht at 12:15 PM on a day where Bourque was booked at 12:00 with a 30-min duration (covering 12:00–12:30). AMD returned HTTP 409 with error 4101: "Can't add appointment: Overlaps existing appointment." The scheduler had shown 12:15 as available because it lumped all overlapping appointments into a single count and compared against `maxApptsPerSlot=2`.

AMD enforces two independent booking conflict rules:

| Error | Rule | Meaning |
|-------|------|---------|
| **4101** | Duration overlap | Cannot book inside another appointment's `[start, start+duration)` — hard block, `maxApptsPerSlot` irrelevant |
| **4186** | Same-start capacity | Too many appointments starting at the exact same time — controlled by `maxApptsPerSlot` |

#### Changed

- **`internal/http/handlers.go`** — Split single `countOverlappingAppointments()` into two functions:
  - `hasOverlappingAppointment()` — checks if any appointment from a DIFFERENT start time extends into the slot (AMD 4101). Runs unconditionally, including for unlimited columns (Dr. Bach, `maxApptsPerSlot=0`). Returns bool — hard block.
  - `countSameStartAppointments()` — counts appointments starting at the EXACT same time (AMD 4186). Only checked when `maxApptsPerSlot > 0`.
- **`internal/http/handlers.go`** — Updated check order in `calculateAvailableSlots()`:
  1. Past-slot filter
  2. Block hold check
  3. `hasOverlappingAppointment` → hard block (4101)
  4. `countSameStartAppointments` vs `maxApptsPerSlot` → capacity block (4186)
- **`CLAUDE.md`** — Updated "How It Works" step 4 to document both checks; added quirk #10 explaining 4101 vs 4186
- **`README.md`** — Added "Slot Availability Logic" section documenting the two conflict checks

#### Fixed

- **Unlimited columns (Dr. Bach) now enforce overlap blocking** — Previous code wrapped the entire overlap check inside `if maxAppts > 0`, which skipped duration-overlap detection for unlimited columns. A multi-slot appointment on Dr. Bach's column would leave overlapped slots falsely available.

#### Updated Tests

- **`internal/http/handlers_test.go`** — Replaced `TestCountOverlappingAppointments` with `TestHasOverlappingAppointment` (7 cases including the Licht 12:15 scenario) + `TestCountSameStartAppointments` (4 cases). Updated `TestCalculateAvailableSlots_MultiSlotAppointment` comments.

---

## [Previous] - 2026-03-04

### Insurance Network Consolidation + Alias Map + Prompt Update

Consolidated all insurance plans from plan-specific carrier IDs to parent network carrier IDs (71 plans → 22 carrier IDs). Added alias map so `LookupInsurance` catches common shorthand. Updated TOOLS.md to group insurance names by network with agent guidance. Fixed `addinsurance` storing insurance as tertiary instead of primary.

#### Changed

- **`internal/clients/advancedmd_xmlrpc.go`** — Fixed `@coverage` from `"3"` (tertiary) to `"1"` (primary) in `AddInsurance` payload
- **`internal/domain/insurance.go`** — Reorganized `InsuranceNameMap` from routing-tier grouping to carrier-ID grouping (8 major networks + standalone). Consolidated carrier IDs:
  - iCare (car40907): 11 plans — Aetna Better Health, Aetna Better Health of Florida, Aetna Healthy Kids, Aetna Medicare HMO, Community Care Plan, Florida Community Care, Florida Complete Care, Miami Children's Health Plan, Simply Medicaid, Vivida, Doctors Health Medicare
  - United Healthcare (car40923): 11 plans — all UHC variants + UMR + Preferred Care Partners
  - Envolve (car281245): 8 plans — Ambetter variants, Children's Medical Services, Envolve Vision, Staywell Medicare, Sunshine Medicaid, Wellcare
  - Humana Consolidated (car308175): 8 plans — all Humana + Molina Medicare + Cigna Medicare Advantage + Molina Marketplace
  - Florida Blue (car40897): 6 plans
  - Cigna (car301345): 5 plans
  - Aetna (car40887): 4 plans
  - Tricare (car40921): 3 plans
  - 14 standalone carriers (1 plan each)
- **`internal/domain/patient_test.go`** — Updated 3 stale carrier IDs to match consolidated values, added 2 new test cases for alias matching ("Oscar" → Oscar Health, "Humana" → Humana PPO)
- **`INSURANCE_CROSSWALK.md`** — Rewritten to organize by carrier ID groupings instead of routing tiers. insurance.go is now the source of truth.
- **`README.md`** — Updated insurance routing summary (71 plans, 22 carrier IDs, alias map)

#### Added

- **`internal/domain/insurance.go`** — `InsuranceAliases` map (26 entries) + updated `LookupInsurance` to check aliases as fallback. Catches common shorthand:
  - "Oscar" → "Oscar Health"
  - "Humana" → "Humana PPO"
  - "Blue Cross" / "BCBS" → "Florida Blue"
  - "United" / "UHC" → "United Healthcare"
  - "Tricare" → "Tricare Select"
  - "Medicare" → "Florida Medicare"
  - "Cigna" → "Cigna PPO"
  - + 19 more aliases
- **`internal/domain/insurance.go`** — 16 new plan entries (all `RoutingAll`):
  - Aetna Healthy Kids, Aetna QHP Individual Exchange, Ambetter Select, Ambetter Value, Children's Medical Services, Cigna Miami-Dade Public Schools, Cigna Open Access, Florida Blue Medicare PPO, Florida Blue PPO Federal Employee, Florida Blue PPO Out of State, Florida Community Care, Medicaid, Miami Children's Health Plan, Staywell Medicare, Sunshine Medicaid, Vivida
- **`internal/workspace/TOOLS.md`** — Insurance section restructured from flat 54-name list to network-grouped format with 70 names and agent guidance (when to ask follow-ups for Molina, Aetna EPO; shorthand tips for Oscar, Humana, Blue Cross)

#### Note

Molina Medicaid also uses iCare network per the insurance list but retains its own carrier ID (`car40912`).

---

## [Previous] - 2026-03-03

### No-Availability Response Guard

When the 14-day auto-search exhausts without finding any open slots, the availability endpoint previously returned the last searched date with `totalAvailable: 0` and empty `slots`. This allowed the LLM to interpret the response as a valid bookable date. Now returns an explicit no-availability response with empty `date`, empty `providers`, and a `message` field.

#### Changed

- **`internal/domain/scheduler.go`** — Added `Message` field (`omitempty`) to `AvailabilityResponse` struct
- **`internal/http/handlers.go`** — After the 14-day search loop, checks if any provider has availability. If none, returns early with empty `date`, empty `providers`, and `"No availability found within 14 days of requested date"` message

#### Added

- **`internal/http/handlers_test.go`** — 4 new tests:
  - `TestCalculateAvailableSlots_AllBlocked` — full-day block hold → 0 slots
  - `TestCalculateAvailableSlots_AllBookedAtMax` — all slots at max capacity → 0 slots
  - `TestNoAvailabilityResponse_HasMessageAndEmptyProviders` — verifies no-availability JSON structure
  - `TestAvailabilityResponse_OmitsMessageWhenEmpty` — verifies `message` omitted when availability exists

---

### Pediatric Routing — Age-Based Provider Override

Patients under 18 are now automatically routed to Dr. Bach (`bach_only`), the only provider who sees pediatrics. Override is applied server-side after insurance routing, and does not override `not_accepted` insurance.

#### Added

- **`internal/domain/patient.go`** — `IsMinor(dob)` function: parses MM/DD/YYYY DOB, returns true if under 18
- **`internal/domain/patient_test.go`** — `TestIsMinor` with 7 cases (adult, child, exactly 18, turns 18 tomorrow, turned 18 yesterday, invalid, empty)

#### Changed

- **`internal/http/handlers.go`** — Pediatric override in 3 spots:
  - verify-patient (single match): overrides routing to `bach_only` + clears `routingAmbiguous`
  - verify-patient (disambiguation match): same override
  - add-patient (success response): overrides `insEntry.Routing` before building response

---

## [Previous] - 2026-02-24

### Insurance Crosswalk — Server-Side Provider Routing

Replaced the generic 7-carrier map (test-environment IDs) with 44 plan-specific entries using live Spring Hill carrier IDs. Insurance routing is now enforced server-side on the availability endpoint.

#### Added

- **`internal/domain/insurance.go`** — New file with all routing logic:
  - `RoutingRule` type with 4 tiers: `not_accepted`, `bach_only`, `bach_licht`, `all_three`
  - `InsuranceNameMap` — 44 insurance plan names → carrier ID + routing rule
  - `CarrierRoutingMap` — carrier ID → routing rule for existing patients (unambiguous carriers only)
  - `AmbiguousCarriers` — 5 shared carrier IDs that span multiple routing tiers
  - `ColumnsForRouting()` — returns allowed column IDs for a routing rule
  - `ProvidersForRouting()` — returns display names for a routing rule
  - `LookupInsurance()` — normalized name lookup for new patients
  - `RoutingForCarrierID()` — returns routing + ambiguity flag for existing patients
  - `ParseRoutingRule()` — parses routing string from request param

- **`verify-patient` response** — New fields: `insuranceCarrierId`, `routing`, `allowedProviders`, `routingAmbiguous`

- **`add-patient` response** — New fields: `routing`, `allowedProviders`

- **`availability` request** — New `routing` parameter filters columns server-side before AMD API calls

#### Changed

- **`internal/domain/patient.go`** — Removed `CarrierMap`, `LookupCarrierID`, `ValidCarrierNames` (replaced by `insurance.go`)

- **`internal/clients/advancedmd_xmlrpc.go`** — `GetDemographic` now returns `(carrierName, carrierID, error)` instead of `(string, error)`

- **`internal/http/handlers.go`**:
  - verify-patient: Populates routing fields from `RoutingForCarrierID()`
  - add-patient: `carrierId` field → `insurance` field; uses `LookupInsurance()` for carrier ID + routing; rejects `not_accepted` insurance
  - availability: Applies `ColumnsForRouting()` filter before fetching AMD data

- **`internal/workspace/files/TOOLS.md`** — Updated verify_patient (routing fields), add_patient (44-name insurance list, `insurance` field), get_availability (`routing` parameter)

#### Fixed

- **`internal/domain/scheduler_test.go`** — Updated stale Spring Hill facility IDs from test env (`1032`) to live (`1568`)
- **`internal/domain/patient_test.go`** — Replaced `TestLookupCarrierID` with `TestLookupInsurance`

---

## [Previous] - 2026-02-19

### Live AMD Keys for Spring Hill

Updated all hardcoded IDs from the test environment to live AMD system (office 139464).

#### Changed

- **AllowedColumns** (`domain/scheduler.go`) — Replaced test column IDs with live Spring Hill columns:
  - Dr. Bach: `1716` → `1513` (profile `1135` → `620`)
  - Dr. Licht: `1723` → `1551` (profile `1141` → `2064`)
  - Dr. Noel: `1726` → `1550` (profile `1137` → `2076`)
  - Removed all non-Spring Hill columns (Hollywood, Sweetwater, Crystal River)

- **Spring Hill facility ID** (`domain/scheduler.go`) — `1032` → `1568`

- **Provider display names** (`http/handlers.go`) — Updated profile ID keys to match live system

- **Booking payload example** (`README.md`) — Updated `columnid`, `profileid`, and `type` format to match live AMD

#### Added

- **`INSURANCE_MAPPING.md`** — Complete insurance-to-provider routing reference for Spring Hill, derived from the Abita Insurance List PDF (rev 9/4/2025) and validated against live AMD carrier data

#### Discovered

- **`getdemographic`** (class=demographics) returns full patient record including insurance (`insplanlist`) and carrier details (`carrierlist`)
- **`lookupcarrier`** (class=api) returns the practice's carrier master list — searchable by name prefix
- **`getappttypes`** (class=masterfiles) returns appointment types when `appttype` field and `@msgtime` are included
- **Live appointment type IDs**: 1006 (New Adult), 1004 (New Pediatric), 1007 (Established Follow Up), 1005 (Established Pediatric), 1008 (Post Op)
- **MaxApptsPerSlot**: Licht and Noel allow 2 per slot in live (was 0 in test). Bach is 0 (unlimited). Current code treats 0 as 1 — may need revisiting.

---

## [Previous] - 2026-02-16

### Availability Endpoint Improvements

Refactored `/api/scheduler/availability` to produce a cleaner, more token-efficient response for ElevenLabs LLM consumption, filter stale slots, and automatically find the next available day when booked.

#### Changed

- **Cleaner response format** — Response optimized for LLM token efficiency:
  - Slots capped at **5 per provider** (with `totalAvailable` for full count)
  - Added `firstAvailable` / `lastAvailable` summary fields
  - Added `searchedDate` (original request) vs `date` (actual result, may differ if auto-expanded)
  - Removed `date` field from individual slots (redundant for single-day search)
  - Removed `schedule` field from providers (verbose, not useful for the LLM)
  - Renamed `availableSlots` → `slots`

- **Past-slot filtering** — If the requested date is today, slots before `now + 30 minutes` Eastern time are excluded. No more offering 8:00 AM when it's already 2:00 PM.

- **Auto-search forward** — When ALL providers have zero availability on the requested date, the endpoint automatically searches day-by-day up to 14 days ahead and returns the first day with any openings. `searchedDate` shows what was requested; `date` shows what was found.

- **`forView=day` instead of `forView=week`** — REST calls to AMD now use `forView=day` since we search one day at a time, reducing response payload size.

- **Removed `days` request parameter** — The endpoint now always searches a single day (with auto-forward on fully booked days), replacing the old multi-day range approach.

#### Fixed

- **Multi-day block holds now fully block all covered days** — AMD's `duration` field on block holds is unreliable for multi-day holds (e.g., a 4-day "OUT OF OFFICE" hold returns `duration: 510` which only covers 8.5 hours, leaving end-of-day slots falsely available). Now uses AMD's `enddatetime` field instead of computing end from `startdatetime + duration`. Previously, a provider marked out Feb 17-20 would still show 4:30/4:45 PM as available on those days.

#### Discovered

- **AMD requires `columnId`** on `/scheduler/appointments` and `/scheduler/blockholds` — bulk calls without it return HTTP 400. Per-column calls remain necessary.

- **AMD block hold `duration` is unreliable** — For multi-day holds, the `duration` field varies depending on which day you query and doesn't consistently cover the provider's full work hours. The `enddatetime` field is the source of truth.

#### Files Modified

| File | Summary |
|------|---------|
| `internal/domain/scheduler.go` | Updated `AvailableSlot`, `ProviderAvailability`, `AvailabilityResponse` structs; removed `FormatSlotDate`; `BlockHold` now uses `EndDateTime` instead of `Duration`; `IsBlockedByHold` uses `EndDateTime` directly |
| `internal/clients/advancedmd_rest.go` | Changed `forView=week` → `forView=day`; parse `enddatetime` from AMD block hold response |
| `internal/http/handlers.go` | Added past-slot filter, auto-search loop, slot cap; removed `buildScheduleDescription`, `formatTimeForDisplay`, `days` parameter |

#### Response Before vs After

**Before** (up to 66 slot objects, verbose):
```json
{
  "date": "Tuesday, February 17, 2026",
  "providers": [{
    "schedule": "Monday-Friday, 8:00 AM - 5:00 PM",
    "availableSlots": [
      {"date": "Tuesday, February 17", "time": "8:00 AM", "datetime": "..."},
      {"date": "Tuesday, February 17", "time": "8:15 AM", "datetime": "..."},
      ...60+ more slots...
    ]
  }]
}
```

**After** (max 5 slots, summary fields):
```json
{
  "searchedDate": "2026-02-17",
  "date": "Tuesday, February 17, 2026",
  "providers": [{
    "totalAvailable": 28,
    "firstAvailable": "8:00 AM",
    "lastAvailable": "4:45 PM",
    "slots": [
      {"time": "8:00 AM", "datetime": "2026-02-17T08:00"},
      {"time": "8:15 AM", "datetime": "2026-02-17T08:15"},
      {"time": "8:30 AM", "datetime": "2026-02-17T08:30"},
      {"time": "8:45 AM", "datetime": "2026-02-17T08:45"},
      {"time": "9:00 AM", "datetime": "2026-02-17T09:00"}
    ]
  }]
}
```
