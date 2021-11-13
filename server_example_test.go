package hx_test

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"time"

	"github.com/go-faster/hx"
)

func ExampleListenAndServe() {
	// The server will listen for incoming requests on this address.
	listenAddr := "127.0.0.1:80"

	// This function will be called by the server for each incoming request.
	//
	// Ctx provides a lot of functionality related to http request
	// processing. See Ctx docs for details.
	requestHandler := func(ctx *hx.Ctx) {
		fmt.Fprintf(ctx, "Hello, world! Requested path is %q", ctx.Path())
	}

	// Start the server with default settings.
	// Create Server instance for adjusting server settings.
	//
	// ListenAndServe returns only on error, so usually it blocks forever.
	if err := hx.ListenAndServe(listenAddr, requestHandler); err != nil {
		log.Fatalf("error in ListenAndServe: %s", err)
	}
}

func ExampleServe() {
	// Create network listener for accepting incoming requests.
	//
	// Note that you are not limited by TCP listener - arbitrary
	// net.Listener may be used by the server.
	// For example, unix socket listener or TLS listener.
	ln, err := net.Listen("tcp4", "127.0.0.1:8080")
	if err != nil {
		log.Fatalf("error in net.Listen: %s", err)
	}

	// This function will be called by the server for each incoming request.
	//
	// Ctx provides a lot of functionality related to http request
	// processing. See Ctx docs for details.
	requestHandler := func(ctx *hx.Ctx) {
		fmt.Fprintf(ctx, "Hello, world! Requested path is %q", ctx.Path())
	}

	// Start the server with default settings.
	// Create Server instance for adjusting server settings.
	//
	// Serve returns on ln.Close() or error, so usually it blocks forever.
	if err := hx.Serve(ln, requestHandler); err != nil {
		log.Fatalf("error in Serve: %s", err)
	}
}

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
	if err := s.ListenAndServe("127.0.0.1:80"); err != nil {
		log.Fatalf("error in ListenAndServe: %s", err)
	}
}

func ExampleRequestCtx_TimeoutError() {
	requestHandler := func(ctx *hx.Ctx) {
		// Emulate long-running task, which touches ctx.
		doneCh := make(chan struct{})
		go func() {
			workDuration := time.Millisecond * time.Duration(rand.Intn(2000))
			time.Sleep(workDuration)

			fmt.Fprintf(ctx, "ctx has been accessed by long-running task\n")
			fmt.Fprintf(ctx, "The reuqestHandler may be finished by this time.\n")

			close(doneCh)
		}()

		select {
		case <-doneCh:
			fmt.Fprintf(ctx, "The task has been finished in less than a second")
		case <-time.After(time.Second):
			// Since the long-running task is still running and may access ctx,
			// we must call TimeoutError before returning from requestHandler.
			//
			// Otherwise the program will suffer from data races.
			ctx.TimeoutError("Timeout!")
		}
	}

	if err := hx.ListenAndServe(":80", requestHandler); err != nil {
		log.Fatalf("error in ListenAndServe: %s", err)
	}
}

func ExampleRequestCtx_Logger() {
	requestHandler := func(ctx *hx.Ctx) {
		if string(ctx.Path()) == "/top-secret" {
			ctx.Logger().Printf("Alarm! Alien intrusion detected!")
			ctx.Error("Access denied!", hx.StatusForbidden)
			return
		}

		// Logger may be cached in local variables.
		logger := ctx.Logger()

		logger.Printf("Good request from User-Agent %q", ctx.Request.Header.UserAgent())
		fmt.Fprintf(ctx, "Good request to %q", ctx.Path())
		logger.Printf("Multiple log messages may be written during a single request")
	}

	if err := hx.ListenAndServe(":80", requestHandler); err != nil {
		log.Fatalf("error in ListenAndServe: %s", err)
	}
}
