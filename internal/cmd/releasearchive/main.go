package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/twentyideas/changesaga/internal/releasearchive"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: releasearchive <output> <epoch> <stage> <binary>")
		os.Exit(2)
	}
	epoch, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil || epoch < 0 {
		fmt.Fprintln(os.Stderr, "releasearchive: epoch must be a non-negative integer")
		os.Exit(2)
	}
	if err := releasearchive.Write(os.Args[1], epoch, os.Args[3], os.Args[4]); err != nil {
		fmt.Fprintf(os.Stderr, "releasearchive: %v\n", err)
		os.Exit(1)
	}
}
