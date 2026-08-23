# Migrating from v1 to v2

**English** · [Русский](MIGRATION.ru.md)

v2 is a new module path, so nothing breaks until you choose to upgrade. v1
keeps resolving at `github.com/efureev/go-swagger-merger`; v2 lives at
`github.com/efureev/go-swagger-merger/v2`.

## Why the rewrite

v1 produced a document that was, in the general case, wrong:

- **Operations were lost.** `paths` merging was commented out, so a whole path
  item was replaced. Merging `{/users: get}` with `{/users: post}` produced a
  document with only `post`, and said nothing.
- **Output was not reproducible.** The document round-tripped through
  `map[string]any`, so key order came from Go's randomised map iteration. Two
  runs over the same inputs produced different files, which makes a committed
  artefact undiffable.
- **A second merge in one process silently dropped data.** Server and tag
  deduplication used package-level maps, so the second `Merger` created in a
  process saw every server and tag as already present.
- **Ordinary input crashed it.** Unchecked type assertions in eight places
  meant an empty file, `servers: null`, or a server without a `url` panicked.
- **Conflicts were invisible.** Two files defining `User` differently kept one
  at random, with no warning.

## Command line

The v1 form still works:

```shell
swagger-merger -o ./docs/swagger.yml -i ./docs/a.yml -i ./docs/b.yml
```

The v2 form is shorter, and `-o` now defaults to stdout:

```shell
swagger-merger merge -o ./docs/swagger.yml ./docs/a.yml ./docs/b.yml
```

What changed:

| v1 | v2 |
| --- | --- |
| Binary `go-swagger-merger` | Binary `swagger-merger` |
| No subcommands | `merge`, `validate`, `version`, `help` |
| `-o` required a file | `-o` defaults to `-` (stdout) |
| Conflicts resolved silently | Conflicts are errors; `--on-conflict=first` restores v1-ish behaviour |
| `panic` with a stack trace | An error message and exit code `1` |
| Always YAML | `--format json`, or infer from the `-o` extension |
| No validation | Local `$ref`s are checked; `validate` reports more |

The one intentional break is the default conflict policy. If you relied on v1
quietly keeping the first definition:

```shell
swagger-merger merge --on-conflict=first -o out.yml a.yml b.yml
```

Expect that to surface real problems the first time you run it.

## Library

```go
// v1
import "github.com/efureev/go-swagger-merger/merger"

m := merger.NewMerger()
for _, f := range files {
    if err := m.AddFile(f); err != nil {
        panic(err)
    }
}
err := m.Save("out.yaml")
```

```go
// v2
import "github.com/efureev/go-swagger-merger/v2/merge"

res, err := merge.Merge(ctx, merge.Options{}, merge.FileSources(files...)...)
if err != nil {
    return err
}
out, err := res.Document.YAML(2)
if err != nil {
    return err
}
return os.WriteFile("out.yaml", out, 0o644)
```

| v1 | v2 |
| --- | --- |
| `merger.NewMerger()` | `merge.New(opts)` or `merge.Merge(ctx, opts, srcs...)` |
| `m.AddFile(path)` | `m.Add(ctx, merge.FileSource(path))` |
| `m.Save(path)` | `res.Document.YAML(2)`, then write it yourself |
| `m.Swagger` (`map[string]any`) | `res.Document.Node()` (`*yaml.Node`) or `Document.Decode(&v)` |
| — | `res.Conflicts`, `res.Diagnostics`, `res.Warnings()` |

Other differences worth knowing:

- **Nothing panics.** Every failure is an error wrapping a sentinel
  (`merge.ErrConflict`, `merge.ErrDanglingRef`, `merge.ErrVersionMismatch`,
  `merge.ErrInvalidDocument`, `merge.ErrNoInput`, `merge.ErrStrict`) and
  carrying a file, line, column and JSON pointer.
- **No global state.** Independent `Merger` values never interfere, and
  concurrent merges are safe.
- **Comments and key order survive**, because the document stays a
  `*yaml.Node` instead of becoming a map.
- **Output is deterministic.** Same inputs, same bytes.
- **`merge.ToString` is gone.** It was an exported helper with one internal
  caller that did not need it.

## Docker

The v1 image declared no `ENTRYPOINT`, so `docker run` did nothing. In v2:

```shell
docker run --rm -v "$PWD:/data" ghcr.io/efureev/go-swagger-merger \
  merge -o /data/swagger.yml /data/a.yml /data/b.yml
```

The image is now `distroless/static`, runs as non-root, and is published for
both `linux/amd64` and `linux/arm64`.

## What v2 does not do

External `$ref` resolution (`./common.yaml#/components/schemas/Error`) and
automatic renaming of conflicting definitions are out of scope. External
references are reported and left untouched; conflicts are surfaced for you to
resolve with a policy.
