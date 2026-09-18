# Insurance participation and billing mappings

Participation outcomes follow the per-location local-agent references at
`abita_agent@0e77f0f6f38e12346ca3ed66b36eae910e769b55`: accepted, prior authorization,
not accepted, or needs clarification. Updated carrier identities must not silently
change those outcomes.

`MEDICAL.json` keeps carrier IDs/codes and aliases at plan level. Participation
and authorization requirements belong to each office policy. A document note or
billing-code discrepancy alone must not turn an accepted plan into staff review.

Acceptance is distinct from readiness to write a chart. A missing carrier ID or
routing mapping still prevents registration and scheduling. Those action failures
must describe the billing prerequisite, rather than repeat a successful acceptance
answer. The agent preserves the middleware decision and applies the same distinction
before attempting registration or an insurance update.

## Verification

`go test ./internal/domain -run TestLegacyOfficeInsuranceOutcomes -count=1`
replays the frozen legacy reference names and aliases in
`internal/domain/testdata/insurance_legacy_outcomes.json`.

| Location | Legacy inputs | Before: changed outcomes | After: changed outcomes |
|---|---:|---:|---:|
| Spring Hill | 247 | 53 | 0 |
| Crystal River | 99 | 7 | 0 |
| Hollywood | 323 | 79 | 0 |
| Sweetwater | 323 | 85 | 0 |
| North Miami Beach Optical | 104 | 0 | 0 |

These 1,096 inputs include aliases, not 1,096 distinct plans. The same suite covers
8 unsupported-office/coverage checks and 10 unknown-plan checks. Prior-authorization
outcomes remain office-specific, including Aetna HMO in Hollywood and Sweetwater.

Carrier IDs, codes, directory labels, aliases, credentialed providers, and office
routing were compared with middleware `6f147b3` and remain unchanged. The replay of
1,578 inputs (including new mapping names) also passes the Python agent's decision
schema. This is local deterministic evidence, not deployment or live voice proof.

## Expanded review coverage

Subagent review also replayed names introduced by the updated mapping. The
`insurance_mapping_outcomes.json` fixture adds 255 inputs that the old matcher
recognized, plus 14 explicit alias-policy cases across 10 office/plan groups.

For those 14 cases, the new mapping identifies a plan whose explicit old office
policy is more restrictive than an incidental match to a broad insurer name.
The explicit policy wins: the mapping must not turn a known rejection or prior
authorization into acceptance through a different alias. Each case records the
old incidental outcome, the mapped plan, the retained policy, and its rationale.
Examples include Spring Hill Humana Medicare HMO/Humana Gold and Hollywood BCBS
Medicare HMO/Florida Blue Medicare HMO. No billing alias is removed or changed.

The remaining 195 newly mapped inputs were unknown to the old matcher. They are
intentional identity additions, not claims of legacy outcome parity. The fixtures
now assert 1,383 cases in total, including unsupported scopes and unknown plans.

Registration and scheduling still require a valid carrier mapping and routing.
An accepted response does not imply those prerequisites or active coverage have
been verified. Existing unresolved mappings are not filled in by guessing.
