# Multi-Office Support

## How Office Routing Works

Each ElevenLabs/LiveKit agent serves one office. When a call comes in, the middleware automatically determines which office it's for using the **phone number the caller dialed** (the SIP trunk number).

### Call Flow

1. Call arrives → LiveKit extracts `sip.trunkPhoneNumber` (e.g., `+17275919997`)
2. `main.ts` calls `setOffice(trunkPhone)` once at call start
3. Every tool call automatically includes `"office": "+17275919997"` in the request body
4. Middleware's `LookupOffice()` strips to digits → `17275919997` → looks up `PhoneToOffice` map → resolves to `spring_hill`
5. The resolved `OfficeConfig` determines: facility ID, allowed providers, routing tiers, pediatric routing, appointment colors, and profile ID for new patients

### Where Office Config Lives

All office configuration is in **one file**: `internal/domain/office.go`

- `OfficeRegistry` — the full config for each office (facility, columns, providers, routing)
- `OfficeAliases` — name-based aliases (`"sh"` → `"spring_hill"`)
- `PhoneToOffice` — phone number → office mapping (what the agent uses)

### Current Offices

| Office | ID | Facility | Phone | Providers |
|--------|----|----------|-------|-----------|
| Spring Hill | `spring_hill` | 1568 | +1 (727) 591-9997 | Dr. Bach, Dr. Licht, Dr. Noel, Routine Vision |

## Adding a New Office

### Step 1: Get the AMD data

You need these from the live AMD system for the new office:

- **Facility ID** — from `getschedulersetup` response (strip `fac` prefix)
- **Column IDs** — one per provider at that location (strip `col` prefix)
- **Profile IDs** — one per provider (strip `prof` prefix)
- **Default profile ID** — the profile to use when creating new patients (usually the lead provider)
- **Work hours, intervals, workweek** — from `columnsetting` in the setup response
- **Routing tiers** — which providers accept which insurance tiers

### Step 2: Add the office config in `office.go`

Add an entry to `OfficeRegistry`:

```go
"crystal_river": {
    ID:               "crystal_river",
    DisplayName:      "Crystal River",
    FacilityID:       "1033",
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
    PediatricRouting: RoutingBachOnly,
},
```

### Step 3: Add aliases and phone mapping

```go
// In OfficeAliases
"crystal_river":  "crystal_river",
"crystalriver":   "crystal_river",
"crystal river":  "crystal_river",
"crystal":        "crystal_river",
"cr":             "crystal_river",

// In PhoneToOffice
"1XXXXXXXXXX": "crystal_river",  // 11 digits with country code
"XXXXXXXXXX":  "crystal_river",  // 10 digits without
```

### Step 4: Deploy

1. `go test ./...` — verify tests pass
2. Deploy middleware to Railway
3. No changes needed in the LiveKit agent — it already sends the trunk phone number, and the middleware resolves it automatically

### Backward Compatibility

- If `office` is empty or missing in a request, the middleware defaults to Spring Hill
- The CLI accepts `--office` flag; omitting it defaults to Spring Hill
- Existing ElevenLabs agents that don't send `office` continue to work unchanged
