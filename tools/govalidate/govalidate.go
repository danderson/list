package main

import (
	"fmt"
	"os"

	"github.com/publicsuffix/list/tools/internal/parser"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s pslfile\n", os.Args[0])
		os.Exit(1)
	}

	bs, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Reading PSL file: %v", err)
		os.Exit(1)
	}

	psl := parser.Parse(string(bs))

	if len(psl.Errors) > 0 {
		for _, err := range psl.Errors {
			fmt.Println(err)
		}
		os.Exit(1)
	} else {
		fmt.Printf("%q seems to be a valid PSL file.\n", os.Args[1])
	}
}
