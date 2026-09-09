# Provider error diagnostics

Business outcome and HTTP transport status are independent. Existing response
bodies and retry/reconciliation rules are unchanged. In particular, HTTP 200
can carry a failed or partial domain outcome.

The API emits these response headers:

- `X-Request-ID`: correlation ID. Canonical UUID v4 IDs supplied by the agent
  are preserved in logs and responses, including when the agent times out.
  Other supplied IDs retain the existing hashed log representation.
- `X-Abita-Outcome`: request outcome category (for example `provider_failure`).
- `X-Abita-Error-Category`: terminal request provider category, or `none`.
- `X-Abita-Provider-Errors`: JSON array of at most eight provider failures.
  Each entry contains fixed `operation` and `category`, `durationMs`, and optional
  numeric `httpStatus` and string `code`. The only supported fault-code path is
  `PPMDResults.Error.Fault.faultcode`, restricted to short numeric codes.
- `X-Abita-Provider-Error-Count`: total observed provider failures. A count above
  the array length means the diagnostic sample was truncated.

Structured request logs include the same `provider_errors`, total
`provider_error_count`, and middleware `http_status`. Provider diagnostics are
captured before adapter normalization and include errors subsequently recovered
by reconciliation. Their presence does not mean the final domain action failed.
Actual upstream status is captured before response-body reading/parsing, so
malformed HTTP-200 responses and non-200 rejection responses stay distinguishable.

No raw provider messages, bodies, URLs, patient identifiers, or input values
are included. An absent code/status is unknown, not a fabricated category.

Abita Agent retains these diagnostics with each native tool-call ID, including
per-request normalization/retry decisions. Product reads them from the existing
closeout domain receipts and displays them in operator tool details. New agent
and Product versions tolerate older middleware responses without these headers.
