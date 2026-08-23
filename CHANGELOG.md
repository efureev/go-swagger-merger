# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.0.0] — unreleased

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
- Go 1.23 is the minimum.

### Removed

- The `merger` package and its `ToString` helper.
- `docker-compose.yml`; the Makefile runs tools natively.

## [1.0.0]

Initial release.
