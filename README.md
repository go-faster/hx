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
wrk -c 512 -t 16 http://localhost:8082
Running 10s test @ http://localhost:8082
  16 threads and 512 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency   485.65us  527.98us  19.66ms   92.33%
    Req/Sec    70.20k     4.51k   96.98k    72.80%
  11204027 requests in 10.09s, 0.98GB read
  Non-2xx or 3xx responses: 11204027
Requests/sec: 1110293.92
Transfer/sec:     99.53MB
```

```
name          old time/op    new time/op     delta
ServeConn-32     675ns ± 2%      411ns ± 2%  -39.07%  (p=0.000 n=10+10)

name          old speed      new speed       delta
ServeConn-32  78.5MB/s ± 2%  128.9MB/s ± 2%  +64.14%  (p=0.000 n=10+10)
```