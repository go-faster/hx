package main

import (
	"context"
	"flag"
	"log"

	"github.com/go-faster/hx"
	"github.com/go-faster/jx"
)

//go:generate go tool jschemagen -target message.go -typename Message message.schema.json

func main() {
	var arg struct {
		Workers int
		Addr    string
		Mode    string
	}
	flag.StringVar(&arg.Addr, "addr", "localhost:8080", "listen address")
	flag.IntVar(&arg.Workers, "j", 500, "count of workers")
	flag.StringVar(&arg.Mode, "mode", "plaintext", "response mode: plaintext or json")
	flag.Parse()

	if arg.Mode != "plaintext" && arg.Mode != "json" {
		log.Fatalf("unknown mode: %q", arg.Mode)
	}

	s := &hx.Server{
		Workers: arg.Workers,
		Handler: func(ctx *hx.Ctx) {
			switch arg.Mode {
			case "plaintext":
				ctx.Response.AppendBodyString("Hello, World!")
			case "json":
				e := jx.GetEncoder()
				defer jx.PutEncoder(e)

				msg := Message{Message: "Hello, World!"}
				msg.Encode(e)

				ctx.Response.AppendBody(e.Bytes())
			}
		},
	}

	log.Fatal(s.ListenAndServe(context.Background(), arg.Addr))
}
