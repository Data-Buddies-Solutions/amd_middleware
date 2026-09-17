# Scheduling ownership

Middleware owns appointment office, visit type, action tokens, provider
eligibility, and the two provider writes of a reschedule. Python owns caller
preferences, explicit confirmation, private call references, patient-context
checks, and call-local receipts. Insurance policy and TypeScript are unchanged.

Patient reads and booking receipts provide `officeId`, `office`, `visitType`,
`cancellationToken`, and `rescheduleToken`. An unknown visit type remains unknown.
Python never derives mutation authority from provider type IDs or facility text;
missing tokens require a reload. Slot searches send visit intent to middleware,
which applies the existing office and provider policy.

## One reschedule command

`POST /api/appointment/reschedule` takes booking intent, `patientId`, a current
`bookingToken`, and the original `rescheduleToken`. Middleware verifies the
original, books once, rechecks the original, then cancels once. It reuses existing
booking validation and ambiguous-write reconciliation. Both writes use the HTTP
request context; no background worker, storage, or deployment setting is added.

The response is `completed` with both receipts, `partial` with a confirmed
replacement and unconfirmed cancellation, `failed`, or `uncertain`. Python applies
confirmed effects to the captured patient, even after a patient switch or stale
reload. A definite failure permits a fresh search and caller-confirmed attempt.
Partial or uncertain results block further writes until staff reconciliation.

The HTTP write is single-shot. Do not retry it after a lost response. Python
serializes writes and replays receipts within the call. There is no persistent
or cross-instance command deduplication. An independent later call or concurrent
request is not protected by that call-local state. AdvancedMD writes are not
atomic; the original recheck reduces but cannot eliminate concurrent staff edits.

## Verification and rollout

Deploy middleware before Python so the new endpoint and metadata are available.
No extra infrastructure or environment variables are required.

Tests cover authoritative metadata, office policy, success, rejection, ambiguous
writes, changed originals, confirmed no-write retries, partial receipts, timeout
guards, and patient-state propagation. Run Python through real authenticated Go
handlers with only the AdvancedMD provider mocked:

```sh
PYTHON_SCHEDULING_WORKTREE=/absolute/path/to/abita_s2s go test ./internal/scheduling -run TestPythonSchedulingContract -v
```

This optional local runner needs `uv` and `tests/middleware_contract.py` in the
matching Python checkout. Regular Go and Python suites remain independent.
These tests make no live provider writes and do not establish live voice proof.
