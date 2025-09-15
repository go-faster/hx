// Binary main implements techempower benchmark server using hx package.
package main

import (
	"context"
	"flag"
	"log/slog"

	"github.com/go-faster/hx"
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
	flag.Parse()

	slog.Info("starting server",
		slog.String("addr", arg.Addr),
		slog.Int("workers", arg.Workers),
	)

	s := &hx.Server{
		Workers: arg.Workers,
		Handler: func(ctx *hx.Ctx) {
			switch string(ctx.Request.URI().Path()) {
			case "/plaintext":
				ctx.Response.AppendBodyString("Hello, World!")
			case "/json":
				ctx.Response.Header.Add("Content-Type", "application/json")

				msg := Message{Message: "Hello, World!"}
				ctx.JSON.Encode(msg)
				ctx.Response.AppendBody(ctx.JSON.Encoder.Bytes())
			default:
				ctx.Response.SetStatusCode(404)
			}
		},
	}
	if err := s.ListenAndServe(context.Background(), arg.Addr); err != nil {
		slog.Error("Failed to start server", slog.Any("err", err))
	}
}
