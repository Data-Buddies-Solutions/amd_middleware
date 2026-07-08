# Multi-Office Support

The middleware is the source of truth for office routing. The voice agent should
send the current office value when it knows it, usually from the inbound trunk
phone number in LiveKit call/session state. Direct callers can also pass an
office name.

## Lookup Inputs

Every endpoint with an `office` field calls `domain.LookupOffice()`.

Accepted values:

- E.164 trunk number: `+19542872010`
- 11-digit US number: `19542872010`
- 10-digit US number: `9542872010`
- Formatted US number: `(954) 287-2010`
- Office ID: `hollywood`
- Display name: `Hollywood`

If `office` is empty, prod defaults to Spring Hill.

## Current Production Registry

| Phone key | Office | Facility | Default profile |
| --- | --- | ---: | ---: |
| `+17275919997` | Spring Hill | `1568` | `620` |
| `+13523202007` | Crystal River | `1576` | `2064` |
| `+16182265883` | Crystal River placeholder | `1576` | `2064` |
| `+19542872010` | Hollywood | `1480` | `620` |
| `+17864657475` | Sweetwater | `670` | `620` |
| `+17864654845` | Sweetwater | `670` | `620` |
| `+17866134310` | Sweetwater | `670` | `620` |
| `+17864657479` | Sweetwater | `670` | `620` |
| `+17864654836` | Sweetwater | `670` | `620` |
| `+17864654882` | Sweetwater | `670` | `620` |
| `+13055095333` | North Miami Beach Optical | `1582` | `621` |

## Current Scheduler Configuration

### Spring Hill

| Lane | Column | Profile | Provider |
| --- | ---: | ---: | --- |
| Medical | `1513` | `620` | Dr. Bach |
| Medical | `1598` | `620` | Dr. Bach |
| Medical | `1551` | `2064` | Dr. Joseph Licht |
| Medical | `1550` | `2076` | Dr. Noel |
| Routine vision | `1600` | `1983` | Dr. Melissa Otero |

Routing:

- `bach_only`: `1513`, `1598`
- `bach_licht`: `1513`, `1598`, `1551`
- `all_three`: `1513`, `1598`, `1551`, `1550`
- `optical_only`: `1600`

### Crystal River

| Lane | Column | Profile | Provider |
| --- | ---: | ---: | --- |
| Medical | `1593` | `2064` | Dr. Joseph Licht |

Routing:

- `bach_only`: `1593`
- `bach_licht`: `1593`
- `all_three`: `1593`

Crystal River does not have a routine-vision lane in middleware.

### Hollywood

| Lane | Column | Profile | Provider | Minimum age |
| --- | ---: | ---: | --- | ---: |
| Medical | `1268` | `620` | Dr. Bach | 0 |
| Medical | `1478` | `620` | Dr. Bach | 0 |
| Routine vision | `1555` | `2075` | Dr. Farnan | 5 |
| Routine vision | `1510` | `2057` | Dr. Vidal | 7 |
| Routine vision | `1305` | `1993` | Dr. Calero | 4 |

Routing:

- `bach_only`: `1268`, `1478`
- `bach_licht`: `1268`, `1478`
- `all_three`: `1268`, `1478`
- `optical_only`: `1555`, `1510`, `1305`

### Sweetwater

| Lane | Column | Profile | Provider | Minimum age |
| --- | ---: | ---: | --- | ---: |
| Medical | `682` | `620` | Dr. Bach | 0 |
| Medical | `1307` | `620` | Dr. Bach | 0 |
| Routine vision | `1296` | `1996` | Dr. Casas | 7 |
| Routine vision | `1554` | `2075` | Dr. Farnan | 5 |
| Routine vision | `1210` | `1993` | Dr. Calero | 4 |

Routing:

- `bach_only`: `682`, `1307`
- `bach_licht`: `682`, `1307`
- `all_three`: `682`, `1307`
- `optical_only`: `1296`, `1554`, `1210`

### North Miami Beach Optical

| Lane | Column | Profile | Provider | Minimum age |
| --- | ---: | ---: | --- | ---: |
| Routine vision | `1601` | `621` | Dr. Miriam Bach | 0 |

Routing:

- `optical_only`: `1601`

North Miami Beach Optical is routine-vision only. It does not have a medical
lane in middleware.

## Dev Registry

`AMD_ENV=dev` switches the active registry to:

| Phone key | Office | Facility | Default profile |
| --- | --- | ---: | ---: |
| `+14843989071` | Spring Hill dev | `1032` | `1135` |
| `+16182265883` | Crystal River placeholder | `1576` | `2064` |

Appointment type IDs are resolved through `ResolveAppointmentTypeID()`. Prod
IDs pass through unchanged; dev maps the known medical type IDs to dev IDs.

## Adding Or Changing An Office

1. Pull live AMD scheduler setup.
2. Record the facility ID, column IDs, profile IDs, column names, intervals,
   workweek, and max appointments per slot.
3. Add or update the `OfficeConfig` in `internal/domain/office.go`.
4. Add each trunk phone to `prodOffices` or `devOffices`.
5. Add routing tier tests in `internal/domain/office_test.go`.
6. Add endpoint-level tests for user-visible behavior in
   `internal/http/handlers_test.go`.
7. Run `go test ./...`.

## Contract Rules

- Do not hard-code office-specific scheduler IDs in the voice agent.
- The agent passes office context; middleware resolves office config.
- Medical availability defaults to `all_three`.
- Routine vision must pass `routing: "optical_only"`.
- DOB should be passed to availability and booking whenever known. Missing DOB
  excludes age-restricted columns from availability and blocks booking into
  those columns. Under-18 DOBs also apply the office's pediatric routing for
  medical availability and booking.
