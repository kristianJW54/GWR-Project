package main

import (
	"context"
	"fmt"
	"os"
	"sse-server/sse"
)

//=================================================================
// Main application function
//=================================================================

func main() {

	ctx := context.Background()
	if err := sse.Run(ctx, os.Stdout, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

}
