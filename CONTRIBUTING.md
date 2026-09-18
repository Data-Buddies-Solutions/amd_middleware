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
