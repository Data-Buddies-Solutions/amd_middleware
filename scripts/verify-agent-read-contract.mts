import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";
const root = process.env.ABITA_AGENT_REPO;
assert.ok(root, "ABITA_AGENT_REPO must point to an installed agent checkout");
const load = (path: string) => import(pathToFileURL(`${root}/${path}`).href);
const { HttpOwnedMiddleware } = await load("src/clients/owned-middleware.ts");
const base = process.env.SANDBOX_AMD_API_URL;
assert.ok(base, "SANDBOX_AMD_API_URL is required");
assert.ok(
  process.env.SANDBOX_AMD_API_TOKEN,
  "SANDBOX_AMD_API_TOKEN is required",
);
assert.match(
  base,
  /^https:\/\/abita-middleware-sandbox-[a-z0-9.-]+\.run\.app$/,
);
const wire: any[] = [];
const client = new HttpOwnedMiddleware({
  authToken: process.env.SANDBOX_AMD_API_TOKEN,
  middlewareBaseUrl: base,
  officeOverride: "spring_hill",
  fetch: async (input: any, init: any) => {
    const start = Date.now();
    const path = new URL(String(input)).pathname;
    assert.ok(
      ["/api/patient/resolve", "/api/scheduler/slots"].includes(path),
      "read-only probe",
    );
    try {
      const response = await fetch(input, init);
      const data = await response.clone().json();
      const result = {
        path,
        http: response.status,
        durationMs: Date.now() - start,
        status: data.status,
        outcome: data.outcome,
        slots: Array.isArray(data.slots) ? data.slots.length : undefined,
      };
      wire.push(result);
      console.log(JSON.stringify({ event: "http", ...result }));
      return response;
    } catch (error) {
      const result = {
        path,
        durationMs: Date.now() - start,
        error: (error as Error).name,
      };
      wire.push(result);
      console.log(JSON.stringify({ event: "http", ...result }));
      throw error;
    }
  },
});
const office = "+17275919997";
const missing = await client.resolvePatient({
  office,
  identity: { phone: "2025550100" },
});
console.log(
  JSON.stringify({
    event: "negative_lookup",
    status: missing.status,
    reason: missing.reason,
  }),
);
const candidates: any[] = [];
for (const fixture of [
  { firstName: "Avery", lastName: "Codextest", dob: "03/12/1990" },
  { firstName: "Morgan", lastName: "Cedartest", dob: "06/14/1992" },
]) {
  const result = await client.resolvePatient({ office, identity: fixture });
  console.log(
    JSON.stringify({
      event: "fixture_lookup",
      fixture: fixture.lastName,
      status: result.status,
      reason: result.reason,
      appointmentsStatus: result.appointmentsStatus,
    }),
  );
  if (result.status === "verified")
    candidates.push({ ...result, testIdentity: fixture });
}
const fixture = candidates[0];
assert.ok(fixture, "An existing synthetic patient fixture is required");
const availability = await client.getAvailability({
  office,
  dob: fixture.dob ?? "03/12/1990",
  routing: fixture.routing ?? "bach_only",
});
console.log(
  JSON.stringify({
    event: "availability",
    status: availability.status,
    reason: availability.reason,
    slots: availability.slots?.length,
    signedSlots: availability.slots?.filter((s: any) => Boolean(s.bookingToken))
      .length,
  }),
);
assert.equal(missing.status, "not_found");
assert.equal(availability.status, "found");
assert.ok(
  availability.slots.length > 0 &&
    availability.slots.every((s: any) => s.bookingToken),
);
console.log(
  JSON.stringify({ event: "result", passed: true, positivePatient: !!fixture }),
);
