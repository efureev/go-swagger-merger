# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.1.1] — 2026-08-23

Documentation and release plumbing only; the library and the CLI are byte for
byte what 2.1.0 shipped.

### Fixed

- The conflict example ran as a test but appeared nowhere in the published
  documentation: an example attached to a name inside a grouped `var` block is
  not rendered on pkg.go.dev. It is now a variant of `Merge`.
- Publishing the image for an older tag by hand moved `latest` onto it, and
  stamped it with the commit of the branch the run started from rather than
  the one it was built from.

### Added

- Status badges in both READMEs, and a Docker section describing what is
  actually published: pinned tags, paths relative to the `/data` working
  directory, and the `--user` override a Linux bind mount needs because the
  image runs as `nonroot`.

## [2.1.0] — 2026-08-23

No change to the library, the CLI or their behaviour. This release carries a
fix to the release workflow alone; a binary built from 2.0.0 and one built
from 2.1.0 differ only in the version stamped into them.

### Fixed

- The container image could not be published. The GHCR package was created by
  the v1 workflow's personal access token, which has since expired, and it is
  not linked to this repository, so the built-in `GITHUB_TOKEN` is refused
  with `permission_denied: read_package`. The dead token is gone from the
  release path, which now depends on the repository linkage instead and so
  carries no expiring secret. Publishing the image still requires that
  linkage to be granted.

### Added

- The release workflow accepts a manual run with a tag, so the image can be
  published for a tag that already exists without re-tagging and re-running
  GoReleaser against a release that is already out.

## [2.0.0] — 2026-08-23

A full rewrite. The module path is now
`github.com/efureev/go-swagger-merger/v2`; v1 is unaffected.
See [MIGRATION.md](MIGRATION.md).

### Fixed

- **Operations were silently dropped.** `paths` merging was commented out, so a
  path item was replaced wholesale: merging `{/users: get}` with
  `{/users: post}` yielded only `post`. Paths now merge per operation.
- **Output was not reproducible.** The YAML → JSON → YAML round trip through
  `map[string]any` took key order from Go's randomised map iteration. The
  document now stays a `yaml.Node` end to end.
- **A second merge in the same process lost data.** Server and tag dedup lived
  in package-level maps shared by every `Merger`. All state is per-instance.
- **Ordinary input caused a panic.** Unchecked type assertions in eight places
  crashed on empty files, `servers: null`, non-mapping roots, and servers or
  tags missing their identifying field. Every one is now a diagnostic.
- **Conflicts were invisible.** Two files defining the same schema differently
  kept one without a word. Conflicts are now errors by default.
- **The Docker image did nothing.** No `ENTRYPOINT` was set.
- **A failed run could destroy the output.** The output file was truncated
  before the merge was known to succeed, and it is commonly also an input.
  Writes are now atomic.
- **`-h` printed `my string representation`** as the `-i` flag's default.

Found by review before release, and fixed here:

- Reference checking treated any mapping holding a `$ref` key as a reference,
  so a schema whose `example` documented a `$ref`-shaped payload failed the
  merge. Checking is now position-aware.
- `--strict` promoted the unavoidable base-override notice, which made it fail
  on every multi-document merge — including the invocations this README
  recommends for CI.
- A conflict in a path-level `parameters` or `servers` list attributed the
  surviving element to whichever file was being merged at the time.
- A conflict found while deep-merging an extension reported the surviving side
  as unknown.
- A zero-value `merge.Merger` panicked on a nil map.
- Calling `Result` after a further `Add` re-reported the validation
  diagnostics of the earlier documents.
- A skipped empty input counted as a source, so a leading empty file consumed
  the base slot and left the merge without a base document.
- Writing to `/dev/null`, a FIFO or a symlink failed, or replaced the symlink;
  these fall back to a direct write now.
- `ReportUnusedComponents` was silently disabled by `SkipRefValidation`.

### Added

- Library API under `merge/`: `Merge`, `New`/`Add`/`Result`, `Options`,
  `Result`, `Document`, and sources from files, memory, `io.Reader` or `fs.FS`.
- Conflict policies — `error` (default), `first`, `last` — settable globally or
  per section. Structurally identical definitions deduplicate silently.
- Local `$ref` validation, reported with file, line, column and JSON pointer.
- Swagger 2.0, OpenAPI 3.0 and OpenAPI 3.1 support, including `webhooks`, with
  incompatible mixes refused.
- Comment, key-order and scalar-style preservation.
- JSON output with object member order preserved.
- `validate` subcommand, including a report of unreferenced components.
- `--sort` for canonical key order, `--strict`, `--base`, `--dry-run`,
  `--log-format json`, and stdin/stdout support.
- Diagnostics with stable codes, a `Reporter` interface, and errors wrapping
  sentinels for `errors.Is`.
- YAML anchor and merge-key expansion with a node budget that rejects
  billion-laughs input.
- Detection of paths that differ only in template parameter naming.
- Test suite: 25 golden fixtures, determinism and concurrency regressions,
  fuzzing, and CLI tests. Coverage is roughly 87%.
- CI that actually runs tests, the race detector, linting, fuzzing and
  `govulncheck` across Linux, macOS and Windows.
- GoReleaser for cross-platform binaries and a multi-arch image.
- Russian translations of the README and the migration guide
  ([README.ru.md](README.ru.md), [MIGRATION.ru.md](MIGRATION.ru.md)).

### Changed

- Binary renamed from `go-swagger-merger` to `swagger-merger`. The v1 command
  line (`-o` with repeated `-i`, no subcommand) still works.
- `-o` defaults to stdout.
- Docker image is `distroless/static` running as non-root, built for
  `linux/amd64` and `linux/arm64`.
- Go 1.25 is the minimum.

### Removed

- The `merger` package and its `ToString` helper.
- `docker-compose.yml`; the Makefile runs tools natively.

## [1.0.0]

Initial release.
