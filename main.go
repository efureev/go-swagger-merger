package main

import (
	"flag"

	"github.com/efureev/go-swagger-merger/merger"
)

func main() {
	merger := merger.NewMerger()

	var outputFileName string

	flag.StringVar(&outputFileName, "o", "swag.yaml", "")
	flag.Parse()

	for _, f := range flag.Args() {
		err := merger.AddFile(f)
		if err != nil {
			panic(err)
		}
	}

	err := merger.Save(outputFileName)
	if err != nil {
		panic(err)
	}
}
