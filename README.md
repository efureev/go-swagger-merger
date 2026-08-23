# swagger-merger

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

Inputs are merged in order, and `-` reads standard input. The first input supplies `info` and the spec version unless
`--base` names another.

| Flag                    | Meaning                                                      |
|-------------------------|--------------------------------------------------------------|
| `-o`, `--output`        | Output file, or `-` for stdout (default `-`)                 |
| `--format`              | `yaml` or `json`; inferred from the `-o` extension otherwise |
| `--indent`              | Indentation width (default `2`)                              |
| `--on-conflict`         | `error` (default), `first` or `last`                         |
| `--on-conflict-section` | Per-section policy, e.g. `schemas=first` (repeatable)        |
| `--base`                | Input whose `info` block and version win                     |
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

| Section                                                         | Rule                                                          |
|-----------------------------------------------------------------|---------------------------------------------------------------|
| `openapi` / `swagger`                                           | Must be compatible; the highest patch within one minor wins   |
| `info`, `externalDocs`, `host`, `basePath`                      | The base document supplies it; the rest are noted and ignored |
| `servers`                                                       | Deduplicated by `url`                                         |
| `tags`                                                          | Deduplicated by `name`                                        |
| `security`                                                      | Deduplicated by content                                       |
| `schemes`, `consumes`, `produces`                               | Set union, first-appearance order                             |
| `paths`, `webhooks`                                             | Merged per path, then **per operation**                       |
| `components.*`                                                  | Merged per definition name, across all nine component maps    |
| `definitions`, `parameters`, `responses`, `securityDefinitions` | Swagger 2.0 equivalents, merged per name                      |
| `x-*` and anything unknown                                      | Mappings deep-merge; other shapes follow the conflict policy  |

Within a path, `parameters` deduplicate on `(name, in)` and `servers` on `url`, so combining two files that both declare
the path's `{id}` parameter yields one parameter, not two.

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

Sections that accept a policy: `info`, `servers`, `tags`, `paths`, `webhooks`,
`components`, `schemas`, `responses`, `parameters`, `security`, `externalDocs`,
`extensions`, `root`.

`--strict` promotes warnings to errors. It deliberately leaves conflicts resolved by an explicit `--on-conflict` alone:
you already said what to do.

## Docker

```shell
docker pull ghcr.io/efureev/go-swagger-merger:latest

docker run --rm -v "$PWD:/data" ghcr.io/efureev/go-swagger-merger \
  merge -o /data/swagger.yml /data/users.yml /data/orders.yml
```

The image is `distroless/static`, runs as a non-root user and is published for
`linux/amd64` and `linux/arm64`.

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
