# swagger-merger

[![CI](https://github.com/efureev/go-swagger-merger/actions/workflows/ci.yml/badge.svg)](https://github.com/efureev/go-swagger-merger/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/efureev/go-swagger-merger/v2.svg)](https://pkg.go.dev/github.com/efureev/go-swagger-merger/v2)
[![Release](https://img.shields.io/github/v/release/efureev/go-swagger-merger)](https://github.com/efureev/go-swagger-merger/releases/latest)
[![License](https://img.shields.io/github/license/efureev/go-swagger-merger)](LICENSE)

**English** · [Русский](README.ru.md)

Merge several OpenAPI or Swagger documents into one — as a Go library, or from the command line.

```shell
go install github.com/efureev/go-swagger-merger/v2/cmd/swagger-merger@latest
```

```shell
swagger-merger merge -o docs/swagger.yml docs/users.yml docs/orders.yml
```

Merging is per-operation and per-definition, output is byte-for-byte reproducible, comments and key order survive the
round trip, and anything ambiguous is reported with a file, a line and a JSON pointer instead of being resolved
silently.

- [Command line](#command-line)
- [Library](#library)
- [Merge semantics](#merge-semantics)
- [Conflicts](#conflicts)
- [Docker](#docker)
- [GitHub Actions](#github-actions)

Upgrading from v1? See [MIGRATION.md](MIGRATION.md).

## Command line

```
swagger-merger merge     [flags] <input...>   Merge documents
swagger-merger validate  [flags] <input...>   Check without writing
swagger-merger version                        Print version information
```

Inputs are merged in order, and `-` reads standard input. The first input supplies `info` unless `--base` names
another. The result declares the newest spec version any input uses.

| Flag                    | Meaning                                                      |
|-------------------------|--------------------------------------------------------------|
| `--files-from`          | File listing inputs, one per line (repeatable)               |
| `-o`, `--output`        | Output file, or `-` for stdout (default `-`)                 |
| `--format`              | `yaml` or `json`; inferred from the `-o` extension otherwise |
| `--indent`              | Indentation width (default `2`)                              |
| `--on-conflict`         | `error` (default), `first` or `last`                         |
| `--on-conflict-section` | Per-section policy, e.g. `schemas=first` (repeatable)        |
| `--base`                | Input whose `info` block wins                                |
| `--sort`                | Canonical key order instead of input order                   |
| `--no-ref-validation`   | Skip the local `$ref` check                                  |
| `--allow-version-skew`  | Permit mixing 3.0.x with 3.1.x                               |
| `--strict`              | Treat warnings as errors                                     |
| `--allow-empty`         | Skip empty inputs instead of failing                         |
| `--dry-run`             | Merge and report, write nothing                              |
| `-q`, `--quiet`         | Suppress diagnostics and the summary                         |
| `--log-format`          | `text` (default) or `json` for machine-readable diagnostics  |

Exit codes: `0` merged, `1` the merge failed, `2` the command line was wrong.

```shell
# Several documents into one, first one wins on ambiguity
swagger-merger merge --on-conflict=first -o api.yaml users.yaml orders.yaml

# Emit JSON, sorted, and fail the build on anything questionable
swagger-merger merge --sort --strict -o api.json *.yaml

# Check in CI without producing a file
swagger-merger validate --strict specs/*.yaml

# Pipelines work
cat base.yaml | swagger-merger merge - extra.yaml > out.yaml
```

### Inputs listed in a file

When the order matters or the list grows long, keep it in a file and pass it with `--files-from`:

```text
# docs/swagger.list — the base comes first
base.yml

users/users.yml        # users
../shared/errors.yml

# orders.yml — disabled for now
```

```shell
swagger-merger merge -o docs/swagger.yml --files-from docs/swagger.list
swagger-merger validate --strict --files-from docs/swagger.list
```

- One path per line, relative to the list's own directory; absolute paths are taken as they are.
- `#` at the start of a line or after a space or tab starts a comment, so `a#b.yml` is still a file name. Blank lines
  are skipped.
- Documents are merged in the order listed, and the first is the base unless `--base` names another.
- `--files-from` can be repeated and combined with `-i` and positional inputs; everything keeps the order it is named
  in, positional inputs last.
- `--files-from -` reads the list from standard input, with paths relative to the working directory.
- A listed document that does not exist is reported with the list's line number.

## Library

Requires Go 1.25 or newer.

```shell
go get github.com/efureev/go-swagger-merger/v2
```

```go
import "github.com/efureev/go-swagger-merger/v2/merge"

res, err := merge.Merge(ctx, merge.Options{},
merge.FileSource("users.yaml"),
merge.FileSource("orders.yaml"),
)
if err != nil {
return err
}
out, err := res.Document.YAML(2)
```

The zero `Options` is the strict configuration: conflicts are errors, local
`$ref`s are validated, input key order is preserved.

Sources come from files, memory, streams or an `fs.FS`, so an embedded spec works without touching the disk:

```go
//go:embed specs/*.yaml
var specs embed.FS

res, err := merge.Merge(ctx, merge.Options{SortKeys: true},
merge.FSSource(specs, "specs/base.yaml"),
merge.FSSource(specs, "specs/extra.yaml"),
)
```

Add documents one at a time when the set is not known up front:

```go
m := merge.New(merge.Options{OnConflict: merge.ConflictFirstWins})
for _, path := range paths {
if err := m.Add(ctx, merge.FileSource(path)); err != nil {
return err
}
}
res, err := m.Result()
```

The result carries the document plus everything the merge noticed:

```go
for _, c := range res.Conflicts {
log.Printf("%s: kept %s, dropped %s", c.Pointer, c.Kept, c.Dropped)
}
for _, d := range res.Warnings() {
log.Printf("%s: %s (%s)", d.Code, d.Message, d.At)
}
```

Errors wrap sentinels, so callers can branch on the cause:

```go
switch {
case errors.Is(err, merge.ErrConflict): // two inputs disagree
case errors.Is(err, merge.ErrDanglingRef): // a $ref lost its target
case errors.Is(err, merge.ErrVersionMismatch): // 2.0 mixed with 3.x
}
```

Render to YAML or JSON, or decode into your own structs:

```go
yamlBytes, err := res.Document.YAML(2)
jsonBytes, err := res.Document.JSON(2) // object member order preserved
err = res.Document.Decode(&mySpecStruct)
```

The library never panics, never writes to a global, and never touches the process — no `os.Exit`, no logging to stdout.
That is enforced by a linter rule, not by convention.

## Merge semantics

| Section                                                         | Rule                                                           |
|-----------------------------------------------------------------|----------------------------------------------------------------|
| `openapi` / `swagger`                                           | Must be compatible; the newest version any input declares wins |
| `info`, `externalDocs`                                          | The base document supplies it; the rest are noted and ignored  |
| `security`, `host`, `basePath`                                  | The last input that sets it replaces it whole, with a warning  |
| `servers`                                                       | Deduplicated by `url`                                          |
| `tags`                                                          | Deduplicated by `name`                                         |
| `schemes`, `consumes`, `produces`                               | Set union, first-appearance order                              |
| `paths`, `webhooks`                                             | Merged per path, then **per operation**                        |
| `components.*`                                                  | Merged per definition name, across all nine component maps     |
| `definitions`, `parameters`, `responses`, `securityDefinitions` | Swagger 2.0 equivalents, merged per name                       |
| `x-*` and anything unknown                                      | Mappings deep-merge; other shapes follow the conflict policy   |

Within a path, `parameters` deduplicate on `(name, in)` and `servers` on `url`, so combining two files that both declare
the path's `{id}` parameter yields one parameter, not two. A key with no value, such as `schemas:` with nothing under
it, counts as absent: it neither clashes with a definition from another input nor removes one.

Both spec families are supported. Swagger 2.0 and OpenAPI 3.x cannot be merged with each other; 3.0.x and 3.1.x need
`--allow-version-skew`, because 3.1 changed schema semantics.

After merging, every local `$ref` is resolved against the result. A reference that lost its target is an error with the
exact location, which is the failure mode merging introduces most often.

## Conflicts

Two inputs defining the same name is the normal case, not the exceptional one. It is only a conflict when the
definitions actually differ — structurally identical ones are deduplicated in silence, so a shared `Error` schema copied
into five files costs nothing.

When they genuinely differ, the default is to stop:

```
error: merge: conflicting definition: /components/schemas/User is defined differently in two inputs
  at /components/schemas/User
  orders.yaml:17:3
  users.yaml:42:3

hint: pass --on-conflict=first or --on-conflict=last to pick a winner,
      or --on-conflict-section schemas=first to scope it to one section.
```

Pick a winner globally with `--on-conflict`, or per section:

```shell
swagger-merger merge --on-conflict=error --on-conflict-section schemas=first ...
```

Sections that accept a policy: `servers`, `tags`, `paths`, `webhooks`,
`components`, `schemas`, `responses`, `parameters`, `extensions`, `root`. Any
other name is refused. `info` and `externalDocs` take no policy, since they come
from the base document, and neither does `security`, which the last input
replaces whole.

`--strict` promotes warnings to errors. It deliberately leaves conflicts resolved by an explicit `--on-conflict` alone,
and version skew permitted by `--allow-version-skew`: you already said what to do.

## Docker

Published to GHCR for `linux/amd64` and `linux/arm64`. Pull `latest`, which
follows the newest stable release, or pin one of the
[released versions](https://github.com/efureev/go-swagger-merger/releases):

```shell
docker pull ghcr.io/efureev/go-swagger-merger:latest
docker pull ghcr.io/efureev/go-swagger-merger:vX.Y.Z
```

The working directory is `/data`, so mount your specs there and the paths stay
short:

```shell
# Merge to stdout, with the input mounted read-only
docker run --rm -v "$PWD:/data:ro" ghcr.io/efureev/go-swagger-merger \
  merge users.yml orders.yml > swagger.yml

# Check in CI without writing anything
docker run --rm -v "$PWD:/data:ro" ghcr.io/efureev/go-swagger-merger \
  validate --strict users.yml orders.yml
```

To write the file from inside the container with `-o`, run as your own uid.
The image is `distroless/static` and runs as `nonroot` (uid 65532), while a
bind mount on Linux keeps the host's ownership, so without `--user` the write
fails with `permission denied`:

```shell
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/data" \
  ghcr.io/efureev/go-swagger-merger merge -o swagger.yml users.yml orders.yml
```

Docker Desktop on macOS, and on Windows for files on a Windows drive, maps the
ownership itself, so there the flag can be left out. Files inside a WSL 2
distribution behave as on Linux and need it.

## GitHub Actions

```yaml
- uses: actions/setup-go@v7
  with: { go-version: stable }
- run: go install github.com/efureev/go-swagger-merger/v2/cmd/swagger-merger@latest
- run: swagger-merger validate --strict docs/*.yaml
- run: swagger-merger merge --sort -o docs/swagger.yml docs/*.yaml
- run: git diff --exit-code -- docs/swagger.yml   # the output is reproducible
```

That last line works because the output is deterministic: same inputs, same bytes, every run.

## Development

Requires Go 1.25 or newer; that is what `go.mod`, the CI matrix and the
Dockerfile builder all pin, so no build silently pulls a different toolchain.

```shell
make test      # go test ./...
make race      # with the race detector
make lint      # golangci-lint
make fuzz      # 60s of fuzzing
make golden    # regenerate the golden fixtures
make build     # bin/swagger-merger
```

## License

MIT — see [LICENSE](LICENSE).
