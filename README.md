# hx

Work in progress.

Simplified and reworked fork of [fasthttp](https://github.com/valyala/fasthttp).

Intended to serve plaintext HTTP 1.1 behind load balancer and reverse-pory in controlled
environment.

Fully buffer request or response.

## Non-goals
* Client
* Load balancing
* WebSocket
* Streaming, like io.Reader or io.Writer
* Forms
* File 
* HTTP 2.0, 3.0, QUIC
* TLS
* Running without reverse proxy