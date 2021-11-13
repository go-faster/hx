# hx [![](https://img.shields.io/badge/go-pkg-00ADD8)](https://pkg.go.dev/github.com/go-faster/hx#section-documentation) [![](https://img.shields.io/codecov/c/github/go-faster/hx?label=cover)](https://codecov.io/gh/go-faster/hx) [![experimental](https://img.shields.io/badge/-experimental-blueviolet)](https://go-faster.org/docs/projects/status#experimental)

Work in progress.

Simplified and reworked fork of [fasthttp](https://github.com/valyala/fasthttp).

Intended to serve plaintext HTTP 1.1 behind load balancer and reverse-proxy in controlled
environment.

Fully buffers requests and responses.

## Non-goals
* Client
* Load balancing
* WebSocket
* Streaming, like io.Reader or io.Writer
* Forms
* Files
* HTTP 2.0, 3.0, QUIC
* TLS
* Running without reverse proxy