# Multi-Office Support

## How Office Routing Works

Each ElevenLabs/LiveKit agent serves one office. When a call comes in, the middleware automatically determines which office it's for using the **phone number the caller dialed** (the SIP trunk number).

### Call Flow

1. Call arrives → LiveKit extracts `sip.trunkPhoneNumber` (e.g., `+17275919997`)
2. `main.ts` calls `setOffice(trunkPhone)` once at call start
3. Every tool call automatically includes `"office": "+17275919997"` in the request body
4. Middleware's `LookupOffice()` resolves the E.164 trunk phone number (with or without the leading `+`) against the active office registry → resolves to `spring_hill`
5. The resolved `OfficeConfig` determines: facility ID, allowed providers, routing tiers, pediatric routing, appointment colors, and profile ID for new patients

### Where Office Config Lives

All office configuration is in **one file**: `internal/domain/office.go`

- `prodOffices` — production office configs keyed by SIP trunk phone number (E.164)
- `devOffices` — dev office configs keyed by SIP trunk phone number (E.164)
- `OfficeRegistry` — active office map selected by `InitRegistry()`

### Current Offices

| Office | ID | Facility | Phone | Providers |
|--------|----|----------|-------|-----------|
| Spring Hill | `spring_hill` | 1568 | +1 (727) 591-9997 | Dr. Bach, Dr. Licht, Dr. Noel |
| Optical Eyeworks | `optical_eyeworks` | 1505 | +1 (954) 287-2010 | Dr. Otero |
| Beacon Eye | `beacon_eye` | 1487 | +1 (786) 465-7509 | Dr. Casas |
| Crystal River | `crystal_river` | 1576 | +1 (352) 320-2007 | Dr. Licht |

## Adding a New Office

### Step 1: Get the AMD data

You need these from the live AMD system for the new office:

- **Facility ID** — from `getschedulersetup` response (strip `fac` prefix)
- **Column IDs** — one per provider at that location (strip `col` prefix)
- **Profile IDs** — one per provider (strip `prof` prefix)
- **Default profile ID** — the profile to use when creating new patients (usually the lead provider)
- **Provider names** — enough to set `DisplayName`, `ShortName`, and `MatchKey`
- **Trunk phone number** — the E.164 number callers dial for that office
- **Routing decision** — whether the office needs tier-specific provider restrictions, or just `RoutingAll`
- **Pediatric decision** — whether minors route to a specific tier, `RoutingAll`, or `RoutingNotAccepted`

Notes:

- Work hours, intervals, and workweek come live from `getschedulersetup`; they are not stored in the office registry.
- If an office accepts all schedulable patients for the same provider pool, defining only `RoutingAll` is enough. Other routing values fall back to `RoutingAll`.

### Step 2: Add the office config in `office.go`

Add an entry to `prodOffices` (and `devOffices` if you have verified dev AMD IDs), keyed by the office's trunk phone number:

```go
"+13523202007": {
    ID:               "crystal_river",
    DisplayName:      "Crystal River",
    FacilityID:       "1576",
    DefaultProfileID: "XXX",  // lead provider's profile ID
    Columns: map[string]OfficeColumn{
        "COLID1": {ProfileID: "PROFID1", DisplayName: "Dr. Full Name", ShortName: "Dr. Last", MatchKey: "LAST"},
        "COLID2": {ProfileID: "PROFID2", DisplayName: "Dr. Full Name", ShortName: "Dr. Last", MatchKey: "LAST"},
    },
    RoutingTiers: map[RoutingRule][]string{
        RoutingBachOnly:  {"COLID1"},           // adjust tier names if needed
        RoutingBachLicht: {"COLID1", "COLID2"},
        RoutingAll:       {"COLID1", "COLID2"},
    },
    PediatricRouting: RoutingAll,
},
```

If the office does not need tier-specific restrictions, keep only `RoutingAll`:

```go
RoutingTiers: map[RoutingRule][]string{
    RoutingAll: {"COLID1", "COLID2"},
},
```

### Step 3: Deploy

1. `go test ./...` — verify tests pass
2. Deploy middleware to Railway
3. No changes needed in the LiveKit agent — it already sends the trunk phone number, and the middleware resolves it automatically

### Backward Compatibility

- If `office` is empty or missing in a request, the middleware defaults to Spring Hill
- The CLI accepts `--office` flag; omitting it defaults to Spring Hill
- Existing ElevenLabs agents that don't send `office` continue to work unchanged
