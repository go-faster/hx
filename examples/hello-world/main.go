// Package hello-world implements binary for hello-world example.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/go-faster/hx"
)

func main() {
	var arg struct {
		Workers   int
		Addr      string
		PprofAddr string
	}
	flag.StringVar(&arg.Addr, "addr", "localhost:8080", "listen address")
	flag.IntVar(&arg.Workers, "j", 500, "count of workers")
	flag.Parse()
	s := &hx.Server{
		Workers: arg.Workers,
		Handler: func(ctx *hx.Ctx) {
			ctx.Response.AppendBodyString("Hello, world")
		},
	}
	log.Fatal(s.ListenAndServe(context.Background(), arg.Addr))
}
