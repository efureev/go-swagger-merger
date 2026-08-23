// Package merge combines OpenAPI and Swagger documents into one.
//
// Documents are merged per operation and per definition rather than by
// replacing whole objects, so two files that describe different methods of the
// same path both survive. The document is carried as a [gopkg.in/yaml.v3.Node]
// from parse to encode, which keeps key order, comments and scalar styles
// intact and makes the output byte-for-byte reproducible.
//
// The zero [Options] is the recommended strict configuration: conflicts are
// errors, local $refs are validated, and input key order is preserved.
//
//	res, err := merge.Merge(ctx, merge.Options{},
//	    merge.FileSource("users.yaml"),
//	    merge.FileSource("orders.yaml"),
//	)
//	if err != nil {
//	    return err
//	}
//	out, err := res.Document.YAML(2)
//
// # Conflicts
//
// Two inputs defining the same name is ordinary. It is only a conflict when
// the definitions differ: structurally identical ones are deduplicated
// silently, so a shared schema copied into several files costs nothing. A real
// difference follows [Options.OnConflict], which defaults to failing with both
// locations named. [Options.SectionPolicy] scopes that decision to one
// [Section].
//
// # Errors
//
// Nothing in this package panics, exits the process, writes to a global or
// logs to stdout. Failures are errors wrapping a sentinel — [ErrConflict],
// [ErrDanglingRef], [ErrVersionMismatch], [ErrInvalidDocument], [ErrNoInput]
// or [ErrStrict] — and carrying the file, line, column and JSON pointer where
// the problem is. Non-fatal observations arrive as [Diagnostic] values on the
// [Result], or through [Options.Reporter] as they happen.
package merge
