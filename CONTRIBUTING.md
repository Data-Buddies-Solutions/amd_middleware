# Contributing

## Pull request titles and merges

Use squash merges into `main`. Before merging, make the pull request title the
exact Conventional Commit subject that should appear on `main`:

```text
<type>[optional scope][!]: <short description>
```

Examples:

- `feat(scheduling): add same-day cancellation`
- `fix(session): retain the last usable token`
- `deps: update golang.org/x/net`
- `feat!: replace the legacy booking request`
- `ci: add release validation`

`feat`, `fix`, and `deps` are Release Please's normal releasable units:
features propose a minor version, fixes and dependency updates propose a patch,
and `!` or a `BREAKING CHANGE:` footer proposes a major version. Other
Conventional Commit types such as `docs`, `test`, `ci`, and `chore` remain
valid but do not create a release by themselves.

Do not merge feature pull requests with a merge commit or rebase merge.
Release Please reads the commits on `main`; squash merging makes the reviewed
pull request title the single release input. When using GitHub's squash dialog,
confirm the final commit title still exactly matches the pull request title.

Release Please maintains a release pull request after releasable changes reach
`main`. Merge that generated pull request with squash merge after CI passes.
That merge updates the changelog and version manifest; the next Release Please
run creates the `vMAJOR.MINOR.PATCH` tag and GitHub Release.

Production deploys when Release Please creates that stable version tag, not
when a commit lands on `main`. The Cloud Build trigger
`abita-middleware-main-build` (in `acuity-health-prod`, `us-east4`) matches
`^v[0-9]+\.[0-9]+\.[0-9]+$` tags and runs `cloudbuild.yaml` against the tagged
commit. Its historical name is retained for the GitHub check integration.
The Production status workflow tracks that build on the same tag push.
Creating a matching tag manually also triggers production deployment.

The same tag redeploys the staging (simulation) service with production's
image once that build succeeds, so simulations never test an older middleware.
That deploy logs the sandbox in to AdvancedMD again and can interrupt a
simulation running at release time. Every staging deploy sets the image and
`AMD_ENV=sandbox` in the same Cloud Run revision, because the middleware accepts
only `prod` or `sandbox`. The manual staging workflow and `staging-*`
tags still deploy from source for ad hoc testing. Release deploys need three
one-time grants; README.md's "Staging follows each release" section lists the
commands.

## Enforced rules

Each rule below fails a check today. A rule without a check does not belong
here. `scripts/check` runs the Go checks; CI runs the same script, so run it
before opening a pull request.

| Rule | What enforces it |
| --- | --- |
| Each package has a layer and imports only the layers that layer allows | `TestPackagesImportOnlyLowerLayers` in `layers_test.go` |
| Go files carry no code comments; only `//go:` directives are allowed | `TestNoCodeComments` in `comments_test.go` |
| Go files outside `cmd/api` and tests do not dot-import `time` and reference `time.Now` only as `x := time.Now()` where `x` is used solely as the argument of `time.Since` | `TestNoWallClockOutsideMain` in `wall_clock_test.go` |
| `Dockerfile` and `cloudbuild.yaml` build with the `go.mod` toolchain | `TestBuildImagesUseTheGoModToolchain` in `toolchain_test.go` |
| Every tracked Go file is `gofmt` formatted | `scripts/check` step `gofmt` |
| Every function is reachable from `main` or a test | `scripts/check` step `dead code` |
| README.md's HTTP interface tables list every route the router serves and no others | `TestReadmeRouteTableMatchesRouter` in `readme_routes_test.go` |
| Tests pass under the race detector | `scripts/check` step `race tests` |
