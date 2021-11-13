package main

import (
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
	log.Fatal(s.ListenAndServe("localhost:8080"))
}
