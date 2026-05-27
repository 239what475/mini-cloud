package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "mini-cloud external E2E suites were removed with the v7 cloud-plane gRPC-only cutover")
	os.Exit(1)
}
