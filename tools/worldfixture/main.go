// Command worldfixture generates isolated synthetic content for a Docker smoke test.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/MrDuckVC/hakaton-gallery-HoopAndMerge/internal/testworld"
)

func main() {
	dir := flag.String("dir", "", "new empty fixture directory")
	version := flag.String("version", "smoke_v1", "world version")
	flag.Parse()
	if *dir == "" {
		log.Fatal("--dir is required")
	}
	if err := testworld.Write(context.Background(), *dir, *version); err != nil {
		log.Fatal(err)
	}
}
