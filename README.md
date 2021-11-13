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
* Optimizations for idle connections


## Benchmarks

Due to different tradeoffs, direct comparison with `net/http` and `fasthttp` is invalid.

Preliminary results on Ryzen 5950x
```
wrk -c 200 -t 12 http://localhost:8080
Running 10s test @ http://localhost:8080
  12 threads and 200 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency   208.27us    1.86ms  66.27ms   98.56%
    Req/Sec   135.88k     8.35k  175.85k    87.14%
  16293688 requests in 10.10s, 2.23GB read
Requests/sec: 1613299.37
Transfer/sec:    226.17MB
```