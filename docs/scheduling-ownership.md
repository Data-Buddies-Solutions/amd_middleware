# Scheduling ownership and reschedule recovery

Middleware owns provider appointment classification, office identity, action
authorization, scheduling eligibility, and both provider writes of a reschedule.
Python owns caller preferences, explicit confirmation, private call references,
patient-context guards, and receipt reconciliation.

## Observed failures and changed behavior

At middleware `12752fa` and Python `9cc440a`, Python classified appointment IDs
using local lists, guessed cancellation office from facility text, and booked a
replacement before issuing a separate cancellation request. A new provider type,
renamed facility, timeout, or patient switch between writes could leave those
facts or the two-write outcome unclear. Booking receipts had no cancellation
token, forcing newly booked appointments down the text-based fallback.

Patient reads and booking receipts now include `visitType` (`medical`,
`routine_vision`, or absent for an unknown type), `officeId`, `office`,
`cancellationToken`, and `rescheduleToken`. The office ID comes from the owning
provider column; display labels never authorize a mutation. Known canonical
appointment types are classified centrally. Python requests a reload for missing
action tokens and staff review for unknown reschedule visit types.

Slot search accepts `visitType`; middleware applies existing office/routing and
provider policy. Python still supplies the caller's selected office and visit
intent. Insurance policy and the TypeScript agent are unchanged.

## Reschedule command

`POST /api/appointment/reschedule` accepts the booking intent fields plus
`patientId`, a current `bookingToken`, and the original `rescheduleToken`.
The original token supplies the appointment ID, office, time, and preserved type.
The destination must support that type and pass the existing patient, provider,
capacity, token-expiry, and booking revalidation checks.

Responses are structured:

- `completed`: `booking` and `cancellation` receipts prove both effects.
- `partial`: `booking` proves a replacement, but original cancellation is not
  confirmed. Do not book again; reconcile with staff.
- `failed`: the command did not complete; inspect `outcome` and reload appointments
  before another action. A repeated attempt may be replaying an earlier failure.
- `uncertain`: the claim is in progress, interrupted, or a provider result cannot
  be verified. Never automatically repeat a provider write.

GCS create-if-absent claims serialize attempts across instances and restarts.
The key identifies the original patient/office/appointment/start, independent of
refreshed tokens. A different destination for a claimed original returns a
conflict with any proven effects. Identical retries replay the stored receipt.
The replacement receipt is saved before cancellation. Middleware re-reads the
original before booking and again before cancellation, and reuses existing
ambiguous-write reconciliation for both provider mutations.

Claims have no lease expiry or automatic takeover. A crash or storage failure can
leave an uncertain claim or partial receipt; this intentionally blocks automatic
retries. Even definitive failed attempts are retained. Staff can reconcile and
finish the operation in AdvancedMD; do not delete claims as a retry mechanism.
No automated recovery writer is introduced. A completed replacement can itself
be rescheduled using its own new action token.

After a request disconnect, a claimed operation gets up to two minutes to finish.
An instance termination can still interrupt it. AdvancedMD does not offer an
atomic transaction or conditional cancellation: the second identity read narrows,
but cannot eliminate, a concurrent staff change between that read and the write.

## Rollout and verification

1. Provision a private, environment/practice-specific bucket, runtime IAM, and
   `RESCHEDULE_RECEIPTS_BUCKET`. Claims must not be subject to automatic deletion.
2. Deploy middleware and verify storage access plus the new metadata/endpoint.
3. Deploy Python. Old middleware lacks this contract; Python deliberately has no
   guessed-office or two-request fallback.

Local regression tests cover metadata, unsupported office/visit combinations,
concurrency, fresh-instance replay, changed selections, booking rejection,
ambiguous booking/cancellation, partial writes, receipt-persistence failure,
changed originals, missing authorization, and Python state propagation.

Run the real Python owner against Go HTTP handlers and mocked AdvancedMD writes:

```sh
PYTHON_SCHEDULING_WORKTREE=/absolute/path/to/abita_s2s go test ./internal/scheduling -run TestPythonSchedulingContract -v
```

The Python checkout must contain `tests/middleware_contract.py` and have `uv`
available. The default Go suite skips only this cross-repository runner; its
middleware and storage unit tests run normally. No production provider writes,
cloud IAM/storage smoke tests, deployment, or live voice verification are part
of this local proof.
