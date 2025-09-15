// Binary main implements techempower benchmark server using hx package.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	_ "net/http/pprof" // #nosec G108

	"github.com/go-faster/hx"
)

//go:generate go tool jschemagen -target message.go -typename Message message.schema.json

func main() {
	var arg struct {
		Workers int
		Addr    string
		Pprof   string
	}
	flag.StringVar(&arg.Addr, "addr", "localhost:8080", "listen address")
	flag.IntVar(&arg.Workers, "j", 500, "count of workers")
	flag.StringVar(&arg.Pprof, "pprof", "", "pprof listen address (empty to disable)")
	flag.Parse()

	if arg.Pprof != "" {
		go func() {
			slog.Info("starting pprof", slog.String("addr", arg.Pprof))
			//#nosec G114
			if err := http.ListenAndServe(arg.Pprof, nil); err != nil {
				slog.Error("pprof ListenAndServe failed", slog.Any("err", err))
			}
		}()
	}

	slog.Info("starting server",
		slog.String("addr", arg.Addr),
		slog.Int("workers", arg.Workers),
	)

	s := &hx.Server{
		Workers: arg.Workers,
		Name:    "hx",
		Handler: func(ctx *hx.Ctx) {
			switch string(ctx.Request.URI().Path()) {
			case "/plaintext":
				ctx.Response.Header.Add("Content-Type", "text/plain")
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
