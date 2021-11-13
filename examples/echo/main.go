package main

import (
	"context"
	"log"

	"github.com/go-faster/hx"
)

func main() {
	s := &hx.Server{
		Workers: 50,
		Handler: func(ctx *hx.Ctx) {
			ctx.Response.AppendBodyString("Hello, world")
		},
	}
	log.Fatal(s.ListenAndServe(context.Background(), "localhost:8080"))
}
