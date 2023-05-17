package main

import (
	"flag"

	"github.com/efureev/go-swagger-merger/merger"
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return "my string representation"
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func main() {
	merger := merger.NewMerger()

	var outputFileName string
	var inputFileNames arrayFlags

	flag.StringVar(&outputFileName, "o", "swag.yaml", "")
	flag.Var(&inputFileNames, "i", `Input file`)
	flag.Parse()
	for _, f := range inputFileNames {
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
