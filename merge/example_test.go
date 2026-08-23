package merge_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"testing/fstest"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

func ExampleMerge() {
	users := []byte(`
openapi: 3.0.3
info: {title: Shop, version: "1.0"}
paths:
  /users:
    get:
      responses:
        "200": {description: ok}
`)
	orders := []byte(`
openapi: 3.0.3
info: {title: Shop, version: "1.0"}
paths:
  /users:
    post:
      responses:
        "201": {description: created}
`)

	res, err := merge.Merge(context.Background(), merge.Options{},
		merge.BytesSource("users.yaml", users),
		merge.BytesSource("orders.yaml", orders),
	)
	if err != nil {
		log.Fatal(err)
	}

	out, err := res.Document.YAML(2)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(out))

	// Output:
	// openapi: 3.0.3
	// info: {title: Shop, version: "1.0"}
	// paths:
	//   /users:
	//     get:
	//       responses:
	//         "200": {description: ok}
	//     post:
	//       responses:
	//         "201": {description: created}
}

func ExampleMerger() {
	m := merge.New(merge.Options{OnConflict: merge.ConflictFirstWins})

	for _, doc := range []string{
		`{openapi: 3.0.3, info: {title: A, version: "1"}, paths: {}, components: {schemas: {S: {type: integer}}}}`,
		`{openapi: 3.0.3, info: {title: B, version: "1"}, paths: {}, components: {schemas: {S: {type: string}}}}`,
	} {
		if err := m.AddBytes("doc.yaml", []byte(doc)); err != nil {
			log.Fatal(err)
		}
	}

	res, err := m.Result()
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range res.Conflicts {
		fmt.Printf("%s resolved %s\n", c.Pointer, c.Resolution)
	}

	// Output:
	// /components/schemas/S resolved first-wins
}

// A conflict is an error by default, and the error identifies what disagreed.
func ExampleErrConflict() {
	a := []byte(`{openapi: 3.0.3, info: {title: A, version: "1"}, paths: {}, components: {schemas: {S: {type: integer}}}}`)
	b := []byte(`{openapi: 3.0.3, info: {title: A, version: "1"}, paths: {}, components: {schemas: {S: {type: string}}}}`)

	_, err := merge.Merge(context.Background(), merge.Options{},
		merge.BytesSource("a.yaml", a),
		merge.BytesSource("b.yaml", b),
	)

	var mErr *merge.Error
	if errors.Is(err, merge.ErrConflict) && errors.As(err, &mErr) {
		fmt.Println(mErr.Diag.Pointer)
	}

	// Output:
	// /components/schemas/S
}

// Sources need not come from disk, so an embedded specification merges without
// touching the filesystem.
func ExampleFSSource() {
	specs := fstest.MapFS{
		"base.yaml":  {Data: []byte(`{openapi: 3.0.3, info: {title: Embedded, version: "1"}, paths: {}}`)},
		"extra.yaml": {Data: []byte(`{openapi: 3.0.3, paths: {/health: {get: {responses: {"200": {description: ok}}}}}}`)},
	}

	res, err := merge.Merge(context.Background(), merge.Options{SortKeys: true},
		merge.FSSource(specs, "base.yaml"),
		merge.FSSource(specs, "extra.yaml"),
	)
	if err != nil {
		log.Fatal(err)
	}

	var spec struct {
		Info struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
		Paths map[string]any `yaml:"paths"`
	}
	if err := res.Document.Decode(&spec); err != nil {
		log.Fatal(err)
	}
	fmt.Println(spec.Info.Title, len(spec.Paths))

	// Output:
	// Embedded 1
}
