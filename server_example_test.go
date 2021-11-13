package hx_test

import (
	"context"
	"fmt"
	"log"

	"github.com/go-faster/hx"
)

func ExampleServer() {
	// This function will be called by the server for each incoming request.
	//
	// Ctx provides a lot of functionality related to http request
	// processing. See Ctx docs for details.
	requestHandler := func(ctx *hx.Ctx) {
		fmt.Fprintf(ctx, "Hello, world! Requested path is %q", ctx.Path())
	}

	// Create custom server.
	s := &hx.Server{
		Handler: requestHandler,

		// Every response will contain 'Server: My super server' header.
		Name: "My super server",

		// Other Server settings may be set here.
	}

	// Start the server listening for incoming requests on the given address.
	//
	// ListenAndServe returns only on error, so usually it blocks forever.
	if err := s.ListenAndServe(context.Background(), "127.0.0.1:80"); err != nil {
		log.Fatalf("error in ListenAndServe: %s", err)
	}
}
