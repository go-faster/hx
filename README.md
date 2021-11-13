# hx

Work in progress.

Simplified and reworked fork of [fasthttp](https://github.com/valyala/fasthttp).

Intended to be behind reverse-proxy and serve plaintext HTTP 1.1.

Fully buffer request or response.

## Non-goals
* WebSocket
* Streaming, like io.Reader or io.Writer
* Forms
* File 
* HTTP 2.0, 3.0, QUIC
* TLS
* Running without reverse proxy