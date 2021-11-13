package hx

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ServeConn serves HTTP requests from the given connection
// using the given handler.
//
// ServeConn returns nil if all requests from the c are successfully served.
// It returns non-nil error otherwise.
//
// Connection c must immediately propagate all the data passed to Write()
// to the client. Otherwise requests' processing may hang.
//
// ServeConn closes c before returning.
func ServeConn(c net.Conn, handler Handler) error {
	v := serverPool.Get()
	if v == nil {
		v = &Server{}
	}
	s := v.(*Server)
	s.Handler = handler
	err := s.ServeConn(c)
	s.Handler = nil
	serverPool.Put(v)
	return err
}

var serverPool sync.Pool

// Serve serves incoming connections from the given listener
// using the given handler.
//
// Serve blocks until the given listener returns permanent error.
func Serve(ln net.Listener, handler Handler) error {
	s := &Server{
		Handler: handler,
	}
	return s.Serve(ln)
}

// ListenAndServe serves HTTP requests from the given TCP addr
// using the given handler.
func ListenAndServe(addr string, handler Handler) error {
	s := &Server{
		Handler: handler,
	}
	return s.ListenAndServe(addr)
}

// Handler must process incoming requests.
//
// Handler must call ctx.TimeoutError() before returning
// if it keeps references to ctx and/or its' members after the return.
// Consider wrapping Handler into TimeoutHandler if response time
// must be limited.
type Handler func(ctx *Ctx)

// ServeHandler must process tls.Config.NextProto negotiated requests.
type ServeHandler func(c net.Conn) error

// Server implements HTTP server.
//
// Default Server settings should satisfy the majority of Server users.
// Adjust Server settings only if you really understand the consequences.
//
// It is forbidden copying Server instances. Create new Server instances
// instead.
//
// It is safe to call Server methods from concurrently running goroutines.
type Server struct {
	noCopy noCopy //nolint:unused,structcheck

	// Handler for processing incoming requests.
	//
	// Take into account that no `panic` recovery is done by `fasthttp` (thus any `panic` will take down the entire server).
	// Instead the user should use `recover` to handle these situations.
	Handler Handler

	// ErrorHandler for returning a response in case of an error while receiving or parsing the request.
	//
	// The following is a non-exhaustive list of errors that can be expected as argument:
	//   * io.EOF
	//   * io.ErrUnexpectedEOF
	//   * ErrGetOnly
	//   * ErrSmallBuffer
	//   * ErrBodyTooLarge
	//   * ErrBrokenChunks
	ErrorHandler func(ctx *Ctx, err error)

	// HeaderReceived is called after receiving the header
	//
	// non zero RequestConfig field values will overwrite the default configs
	HeaderReceived func(header *RequestHeader) RequestConfig

	// ContinueHandler is called after receiving the Expect 100 Continue Header
	//
	// https://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html#sec8.2.3
	// https://www.w3.org/Protocols/rfc2616/rfc2616-sec10.html#sec10.1.1
	// Using ContinueHandler a server can make decisioning on whether or not
	// to read a potentially large request body based on the headers
	//
	// The default is to automatically read request bodies of Expect 100 Continue requests
	// like they are normal requests
	ContinueHandler func(header *RequestHeader) bool

	// Server name for sending in response headers.
	//
	// Default server name is used if left blank.
	Name string

	// The maximum number of concurrent connections the server may serve.
	//
	// DefaultConcurrency is used if not set.
	//
	// Concurrency only works if you either call Serve once, or only ServeConn multiple times.
	// It works with ListenAndServe as well.
	Concurrency int

	// Per-connection buffer size for requests' reading.
	// This also limits the maximum header size.
	//
	// Increase this buffer if your clients send multi-KB RequestURIs
	// and/or multi-KB headers (for example, BIG cookies).
	//
	// Default buffer size is used if not set.
	ReadBufferSize int

	// Per-connection buffer size for responses' writing.
	//
	// Default buffer size is used if not set.
	WriteBufferSize int

	// ReadTimeout is the amount of time allowed to read
	// the full request including body. The connection's read
	// deadline is reset when the connection opens, or for
	// keep-alive connections after the first byte has been read.
	//
	// By default request read timeout is unlimited.
	ReadTimeout time.Duration

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset after the request handler
	// has returned.
	//
	// By default response write timeout is unlimited.
	WriteTimeout time.Duration

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alive is enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used.
	IdleTimeout time.Duration

	// Period between tcp keep-alive messages.
	//
	// TCP keep-alive period is determined by operation system by default.
	TCPKeepalivePeriod time.Duration

	// Maximum request body size.
	//
	// The server rejects requests with bodies exceeding this limit.
	//
	// Request body size is limited by DefaultMaxRequestBodySize by default.
	MaxRequestBodySize int

	// Whether to enable tcp keep-alive connections.
	//
	// Whether the operating system should send tcp keep-alive messages on the tcp connection.
	//
	// By default tcp keep-alive connections are disabled.
	TCPKeepalive bool

	// Logs all errors, including the most frequent
	// 'connection reset by peer', 'broken pipe' and 'connection timeout'
	// errors. Such errors are common in production serving real-world
	// clients.
	//
	// By default the most frequent errors such as
	// 'connection reset by peer', 'broken pipe' and 'connection timeout'
	// are suppressed in order to limit output log traffic.
	LogAllErrors bool

	// Will not log potentially sensitive content in error logs
	//
	// This option is useful for servers that handle sensitive data
	// in the request/response.
	//
	// Server logs all full errors by default.
	SecureErrorLogMessage bool

	// Header names are passed as-is without normalization
	// if this option is set.
	//
	// Disabled header names' normalization may be useful only for proxying
	// incoming requests to other servers expecting case-sensitive
	// header names. See https://github.com/go-faster/hx/issues/57
	// for details.
	//
	// By default request and response header names are normalized, i.e.
	// The first letter and the first letters following dashes
	// are uppercased, while all the other letters are lowercased.
	// Examples:
	//
	//     * HOST -> Host
	//     * content-type -> Content-Type
	//     * cONTENT-lenGTH -> Content-Length
	DisableHeaderNamesNormalizing bool

	// SleepWhenConcurrencyLimitsExceeded is a duration to be slept of if
	// the concurrency limit in exceeded (default [when is 0]: don't sleep
	// and accept new connections immediately).
	SleepWhenConcurrencyLimitsExceeded time.Duration

	// NoDefaultServerHeader, when set to true, causes the default Server header
	// to be excluded from the Response.
	//
	// The default Server header value is the value of the Name field or an
	// internal default value in its absence. With this option set to true,
	// the only time a Server header will be sent is if a non-zero length
	// value is explicitly provided during a request.
	NoDefaultServerHeader bool

	// NoDefaultDate, when set to true, causes the default Date
	// header to be excluded from the Response.
	//
	// The default Date header value is the current date value. When
	// set to true, the Date will not be present.
	NoDefaultDate bool

	// NoDefaultContentType, when set to true, causes the default Content-Type
	// header to be excluded from the Response.
	//
	// The default Content-Type header value is the internal default value. When
	// set to true, the Content-Type will not be present.
	NoDefaultContentType bool

	// KeepHijackedConns is an opt-in disable of connection
	// close by fasthttp after connections' HijackHandler returns.
	// This allows to save goroutines, e.g. when fasthttp used to upgrade
	// http connections to WS and connection goes to another handler,
	// which will close it when needed.
	KeepHijackedConns bool

	// CloseOnShutdown when true adds a `Connection: close` header when when the server is shutting down.
	CloseOnShutdown bool

	// StreamRequestBody enables request body streaming,
	// and calls the handler sooner when given body is
	// larger then the current limit.
	StreamRequestBody bool

	// ConnState specifies an optional callback function that is
	// called when a client connection changes state. See the
	// ConnState type and associated constants for details.
	ConnState func(net.Conn, ConnState)

	// Logger, which is used by Ctx.Logger().
	//
	// By default standard logger from log package is used.
	Logger Logger

	// TLSConfig optionally provides a TLS configuration for use
	// by ServeTLS, ServeTLSEmbed, ListenAndServeTLS, ListenAndServeTLSEmbed,
	// AppendCert, AppendCertEmbed and NextProto.
	//
	// Note that this value is cloned by ServeTLS, ServeTLSEmbed, ListenAndServeTLS
	// and ListenAndServeTLSEmbed, so it's not possible to modify the configuration
	// with methods like tls.Config.SetSessionTicketKeys.
	// To use SetSessionTicketKeys, use Server.Serve with a TLS Listener
	// instead.
	TLSConfig *tls.Config

	nextProtos map[string]ServeHandler

	concurrency   uint32
	concurrencyCh chan struct{}
	serverName    atomic.Value

	ctxPool        sync.Pool
	readerPool     sync.Pool
	writerPool     sync.Pool
	hijackConnPool sync.Pool

	// We need to know our listeners so we can close them in Shutdown().
	ln []net.Listener

	mu   sync.Mutex
	open int32
	stop int32
	done chan struct{}
}

// TimeoutHandler creates Handler, which returns StatusRequestTimeout
// error with the given msg to the client if h didn't return during
// the given duration.
//
// The returned handler may return StatusTooManyRequests error with the given
// msg to the client if there are more than Server.Concurrency concurrent
// handlers h are running at the moment.
func TimeoutHandler(h Handler, timeout time.Duration, msg string) Handler {
	return TimeoutWithCodeHandler(h, timeout, msg, StatusRequestTimeout)
}

// TimeoutWithCodeHandler creates Handler, which returns an error with
// the given msg and status code to the client  if h didn't return during
// the given duration.
//
// The returned handler may return StatusTooManyRequests error with the given
// msg to the client if there are more than Server.Concurrency concurrent
// handlers h are running at the moment.
func TimeoutWithCodeHandler(h Handler, timeout time.Duration, msg string, statusCode int) Handler {
	if timeout <= 0 {
		return h
	}

	return func(ctx *Ctx) {
		concurrencyCh := ctx.s.concurrencyCh
		select {
		case concurrencyCh <- struct{}{}:
		default:
			ctx.Error(msg, StatusTooManyRequests)
			return
		}

		ch := ctx.timeoutCh
		if ch == nil {
			ch = make(chan struct{}, 1)
			ctx.timeoutCh = ch
		}
		go func() {
			h(ctx)
			ch <- struct{}{}
			<-concurrencyCh
		}()
		ctx.timeoutTimer = initTimer(ctx.timeoutTimer, timeout)
		select {
		case <-ch:
		case <-ctx.timeoutTimer.C:
			ctx.TimeoutErrorWithCode(msg, statusCode)
		}
		stopTimer(ctx.timeoutTimer)
	}
}

// RequestConfig configure the per request deadline and body limits
type RequestConfig struct {
	// ReadTimeout is the maximum duration for reading the entire
	// request body.
	// a zero value means that default values will be honored
	ReadTimeout time.Duration
	// WriteTimeout is the maximum duration before timing out
	// writes of the response.
	// a zero value means that default values will be honored
	WriteTimeout time.Duration
	// Maximum request body size.
	// a zero value means that default values will be honored
	MaxRequestBodySize int
}

// Ctx contains incoming request and manages outgoing response.
//
// It is forbidden to copy Ctx instances.
//
// Handler should avoid holding references to incoming Ctx and/or
// its' members after the return.
// If holding Ctx references after the return is unavoidable
// (for instance, ctx is passed to a separate goroutine and ctx lifetime cannot
// be controlled), then the Handler MUST call ctx.TimeoutError()
// before return.
//
// It is unsafe modifying/reading Ctx instance from concurrently
// running goroutines. The only exception is TimeoutError*, which may be called
// while other goroutines accessing Ctx.
type Ctx struct {
	noCopy noCopy //nolint:unused,structcheck

	// Incoming request.
	//
	// Copying Request by value is forbidden. Use pointer to Request instead.
	Request Request

	// Outgoing response.
	//
	// Copying Response by value is forbidden. Use pointer to Response instead.
	Response Response

	connID         uint64
	connRequestNum uint64
	connTime       time.Time
	remoteAddr     net.Addr

	time time.Time

	logger ctxLogger
	s      *Server
	c      net.Conn
	fbr    firstByteReader

	timeoutResponse *Response
	timeoutCh       chan struct{}
	timeoutTimer    *time.Timer
}

// Value is no-op implementation for context.Context.
func (ctx *Ctx) Value(key interface{}) interface{} {
	return nil
}

// Conn returns a reference to the underlying net.Conn.
//
// WARNING: Only use this method if you know what you are doing!
//
// Reading from or writing to the returned connection will end badly!
func (ctx *Ctx) Conn() net.Conn {
	return ctx.c
}

type firstByteReader struct {
	c        net.Conn
	ch       byte
	byteRead bool
}

func (r *firstByteReader) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	nn := 0
	if !r.byteRead {
		b[0] = r.ch
		b = b[1:]
		r.byteRead = true
		nn = 1
	}
	n, err := r.c.Read(b)
	return n + nn, err
}

// Logger is used for logging formatted messages.
type Logger interface {
	// Printf must have the same semantics as log.Printf.
	Printf(format string, args ...interface{})
}

var ctxLoggerLock sync.Mutex

type ctxLogger struct {
	ctx    *Ctx
	logger Logger
}

func (cl *ctxLogger) Printf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	ctxLoggerLock.Lock()
	cl.logger.Printf("%.3f %s - %s", time.Since(cl.ctx.ConnTime()).Seconds(), cl.ctx.String(), msg)
	ctxLoggerLock.Unlock()
}

var zeroTCPAddr = &net.TCPAddr{
	IP: net.IPv4zero,
}

// String returns unique string representation of the ctx.
//
// The returned value may be useful for logging.
func (ctx *Ctx) String() string {
	return fmt.Sprintf("#%016X - %s<->%s - %s %s", ctx.ID(), ctx.LocalAddr(), ctx.RemoteAddr(), ctx.Request.Header.Method(), ctx.URI().FullURI())
}

// ID returns unique ID of the request.
func (ctx *Ctx) ID() uint64 {
	return (ctx.connID << 32) | ctx.connRequestNum
}

// ConnID returns unique connection ID.
//
// This ID may be used to match distinct requests to the same incoming
// connection.
func (ctx *Ctx) ConnID() uint64 {
	return ctx.connID
}

// Time returns Handler call time.
func (ctx *Ctx) Time() time.Time {
	return ctx.time
}

// ConnTime returns the time the server started serving the connection
// the current request came from.
func (ctx *Ctx) ConnTime() time.Time {
	return ctx.connTime
}

// ConnRequestNum returns request sequence number
// for the current connection.
//
// Sequence starts with 1.
func (ctx *Ctx) ConnRequestNum() uint64 {
	return ctx.connRequestNum
}

// SetConnectionClose sets 'Connection: close' response header and closes
// connection after the Handler returns.
func (ctx *Ctx) SetConnectionClose() {
	ctx.Response.SetConnectionClose()
}

// SetStatusCode sets response status code.
func (ctx *Ctx) SetStatusCode(statusCode int) {
	ctx.Response.SetStatusCode(statusCode)
}

// SetContentType sets response Content-Type.
func (ctx *Ctx) SetContentType(contentType string) {
	ctx.Response.Header.SetContentType(contentType)
}

// SetContentTypeBytes sets response Content-Type.
//
// It is safe modifying contentType buffer after function return.
func (ctx *Ctx) SetContentTypeBytes(contentType []byte) {
	ctx.Response.Header.SetContentTypeBytes(contentType)
}

// RequestURI returns RequestURI.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) RequestURI() []byte {
	return ctx.Request.Header.RequestURI()
}

// URI returns requested uri.
//
// This uri is valid until your request handler returns.
func (ctx *Ctx) URI() *URI {
	return ctx.Request.URI()
}

// Referer returns request referer.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) Referer() []byte {
	return ctx.Request.Header.Referer()
}

// UserAgent returns User-Agent header value from the request.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) UserAgent() []byte {
	return ctx.Request.Header.UserAgent()
}

// Path returns requested path.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) Path() []byte {
	return ctx.URI().Path()
}

// Host returns requested host.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) Host() []byte {
	return ctx.URI().Host()
}

// QueryArgs returns query arguments from RequestURI.
//
// It doesn't return POST'ed arguments - use PostArgs() for this.
//
// See also PostArgs, FormValue and FormFile.
//
// These args are valid until your request handler returns.
func (ctx *Ctx) QueryArgs() *Args {
	return ctx.URI().QueryArgs()
}

// PostArgs returns POST arguments.
//
// It doesn't return query arguments from RequestURI - use QueryArgs for this.
//
// See also QueryArgs, FormValue and FormFile.
//
// These args are valid until your request handler returns.
func (ctx *Ctx) PostArgs() *Args {
	return ctx.Request.PostArgs()
}

// IsGet returns true if request method is GET.
func (ctx *Ctx) IsGet() bool {
	return ctx.Request.Header.IsGet()
}

// IsPost returns true if request method is POST.
func (ctx *Ctx) IsPost() bool {
	return ctx.Request.Header.IsPost()
}

// IsPut returns true if request method is PUT.
func (ctx *Ctx) IsPut() bool {
	return ctx.Request.Header.IsPut()
}

// IsDelete returns true if request method is DELETE.
func (ctx *Ctx) IsDelete() bool {
	return ctx.Request.Header.IsDelete()
}

// IsConnect returns true if request method is CONNECT.
func (ctx *Ctx) IsConnect() bool {
	return ctx.Request.Header.IsConnect()
}

// IsOptions returns true if request method is OPTIONS.
func (ctx *Ctx) IsOptions() bool {
	return ctx.Request.Header.IsOptions()
}

// IsTrace returns true if request method is TRACE.
func (ctx *Ctx) IsTrace() bool {
	return ctx.Request.Header.IsTrace()
}

// IsPatch returns true if request method is PATCH.
func (ctx *Ctx) IsPatch() bool {
	return ctx.Request.Header.IsPatch()
}

// Method return request method.
//
// Returned value is valid until your request handler returns.
func (ctx *Ctx) Method() []byte {
	return ctx.Request.Header.Method()
}

// IsHead returns true if request method is HEAD.
func (ctx *Ctx) IsHead() bool {
	return ctx.Request.Header.IsHead()
}

// RemoteAddr returns client address for the given request.
//
// Always returns non-nil result.
func (ctx *Ctx) RemoteAddr() net.Addr {
	if ctx.remoteAddr != nil {
		return ctx.remoteAddr
	}
	if ctx.c == nil {
		return zeroTCPAddr
	}
	addr := ctx.c.RemoteAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// SetRemoteAddr sets remote address to the given value.
//
// Set nil value to resore default behaviour for using
// connection remote address.
func (ctx *Ctx) SetRemoteAddr(remoteAddr net.Addr) {
	ctx.remoteAddr = remoteAddr
}

// LocalAddr returns server address for the given request.
//
// Always returns non-nil result.
func (ctx *Ctx) LocalAddr() net.Addr {
	if ctx.c == nil {
		return zeroTCPAddr
	}
	addr := ctx.c.LocalAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// RemoteIP returns the client ip the request came from.
//
// Always returns non-nil result.
func (ctx *Ctx) RemoteIP() net.IP {
	return addrToIP(ctx.RemoteAddr())
}

// LocalIP returns the server ip the request came to.
//
// Always returns non-nil result.
func (ctx *Ctx) LocalIP() net.IP {
	return addrToIP(ctx.LocalAddr())
}

func addrToIP(addr net.Addr) net.IP {
	x, ok := addr.(*net.TCPAddr)
	if !ok {
		return net.IPv4zero
	}
	return x.IP
}

// Error sets response status code to the given value and sets response body
// to the given message.
//
// Warning: this will reset the response headers and body already set!
func (ctx *Ctx) Error(msg string, statusCode int) {
	ctx.Response.Reset()
	ctx.SetStatusCode(statusCode)
	ctx.SetContentTypeBytes(defaultContentType)
	ctx.SetBodyString(msg)
}

// Success sets response Content-Type and body to the given values.
func (ctx *Ctx) Success(contentType string, body []byte) {
	ctx.SetContentType(contentType)
	ctx.SetBody(body)
}

// SuccessString sets response Content-Type and body to the given values.
func (ctx *Ctx) SuccessString(contentType, body string) {
	ctx.SetContentType(contentType)
	ctx.SetBodyString(body)
}

// Redirect sets 'Location: uri' response header and sets the given statusCode.
//
// statusCode must have one of the following values:
//
//    * StatusMovedPermanently (301)
//    * StatusFound (302)
//    * StatusSeeOther (303)
//    * StatusTemporaryRedirect (307)
//    * StatusPermanentRedirect (308)
//
// All other statusCode values are replaced by StatusFound (302).
//
// The redirect uri may be either absolute or relative to the current
// request uri. Fasthttp will always send an absolute uri back to the client.
// To send a relative uri you can use the following code:
//
//   strLocation = []byte("Location") // Put this with your top level var () declarations.
//   ctx.Response.Header.SetCanonical(strLocation, "/relative?uri")
//   ctx.Response.SetStatusCode(fasthttp.StatusMovedPermanently)
//
func (ctx *Ctx) Redirect(uri string, statusCode int) {
	u := AcquireURI()
	ctx.URI().CopyTo(u)
	u.Update(uri)
	ctx.redirect(u.FullURI(), statusCode)
	ReleaseURI(u)
}

// RedirectBytes sets 'Location: uri' response header and sets
// the given statusCode.
//
// statusCode must have one of the following values:
//
//    * StatusMovedPermanently (301)
//    * StatusFound (302)
//    * StatusSeeOther (303)
//    * StatusTemporaryRedirect (307)
//    * StatusPermanentRedirect (308)
//
// All other statusCode values are replaced by StatusFound (302).
//
// The redirect uri may be either absolute or relative to the current
// request uri. Fasthttp will always send an absolute uri back to the client.
// To send a relative uri you can use the following code:
//
//   strLocation = []byte("Location") // Put this with your top level var () declarations.
//   ctx.Response.Header.SetCanonical(strLocation, "/relative?uri")
//   ctx.Response.SetStatusCode(fasthttp.StatusMovedPermanently)
//
func (ctx *Ctx) RedirectBytes(uri []byte, statusCode int) {
	s := b2s(uri)
	ctx.Redirect(s, statusCode)
}

func (ctx *Ctx) redirect(uri []byte, statusCode int) {
	ctx.Response.Header.SetCanonical(strLocation, uri)
	statusCode = getRedirectStatusCode(statusCode)
	ctx.Response.SetStatusCode(statusCode)
}

func getRedirectStatusCode(statusCode int) int {
	if statusCode == StatusMovedPermanently || statusCode == StatusFound ||
		statusCode == StatusSeeOther || statusCode == StatusTemporaryRedirect ||
		statusCode == StatusPermanentRedirect {
		return statusCode
	}
	return StatusFound
}

// SetBody sets response body to the given value.
//
// It is safe re-using body argument after the function returns.
func (ctx *Ctx) SetBody(body []byte) {
	ctx.Response.SetBody(body)
}

// SetBodyString sets response body to the given value.
func (ctx *Ctx) SetBodyString(body string) {
	ctx.Response.SetBodyString(body)
}

// ResetBody resets response body contents.
func (ctx *Ctx) ResetBody() {
	ctx.Response.ResetBody()
}

// IfModifiedSince returns true if lastModified exceeds 'If-Modified-Since'
// value from the request header.
//
// The function returns true also 'If-Modified-Since' request header is missing.
func (ctx *Ctx) IfModifiedSince(lastModified time.Time) bool {
	ifModStr := ctx.Request.Header.peek(strIfModifiedSince)
	if len(ifModStr) == 0 {
		return true
	}
	ifMod, err := ParseHTTPDate(ifModStr)
	if err != nil {
		return true
	}
	lastModified = lastModified.Truncate(time.Second)
	return ifMod.Before(lastModified)
}

// NotModified resets response and sets '304 Not Modified' response status code.
func (ctx *Ctx) NotModified() {
	ctx.Response.Reset()
	ctx.SetStatusCode(StatusNotModified)
}

// NotFound resets response and sets '404 Not Found' response status code.
func (ctx *Ctx) NotFound() {
	ctx.Response.Reset()
	ctx.SetStatusCode(StatusNotFound)
	ctx.SetBodyString("404 Page not found")
}

// Write writes p into response body.
func (ctx *Ctx) Write(p []byte) (int, error) {
	ctx.Response.AppendBody(p)
	return len(p), nil
}

// WriteString appends s to response body.
func (ctx *Ctx) WriteString(s string) (int, error) {
	ctx.Response.AppendBodyString(s)
	return len(s), nil
}

// PostBody returns POST request body.
//
// The returned bytes are valid until your request handler returns.
func (ctx *Ctx) PostBody() []byte {
	return ctx.Request.Body()
}

// Logger returns logger, which may be used for logging arbitrary
// request-specific messages inside Handler.
//
// Each message logged via returned logger contains request-specific information
// such as request id, request duration, local address, remote address,
// request method and request url.
//
// It is safe re-using returned logger for logging multiple messages
// for the current request.
//
// The returned logger is valid until your request handler returns.
func (ctx *Ctx) Logger() Logger {
	if ctx.logger.ctx == nil {
		ctx.logger.ctx = ctx
	}
	if ctx.logger.logger == nil {
		ctx.logger.logger = ctx.s.logger()
	}
	return &ctx.logger
}

// TimeoutError sets response status code to StatusRequestTimeout and sets
// body to the given msg.
//
// All response modifications after TimeoutError call are ignored.
//
// TimeoutError MUST be called before returning from Handler if there are
// references to ctx and/or its members in other goroutines remain.
//
// Usage of this function is discouraged. Prefer eliminating ctx references
// from pending goroutines instead of using this function.
func (ctx *Ctx) TimeoutError(msg string) {
	ctx.TimeoutErrorWithCode(msg, StatusRequestTimeout)
}

// TimeoutErrorWithCode sets response body to msg and response status
// code to statusCode.
//
// All response modifications after TimeoutErrorWithCode call are ignored.
//
// TimeoutErrorWithCode MUST be called before returning from Handler
// if there are references to ctx and/or its members in other goroutines remain.
//
// Usage of this function is discouraged. Prefer eliminating ctx references
// from pending goroutines instead of using this function.
func (ctx *Ctx) TimeoutErrorWithCode(msg string, statusCode int) {
	var resp Response
	resp.SetStatusCode(statusCode)
	resp.SetBodyString(msg)
	ctx.TimeoutErrorWithResponse(&resp)
}

// TimeoutErrorWithResponse marks the ctx as timed out and sends the given
// response to the client.
//
// All ctx modifications after TimeoutErrorWithResponse call are ignored.
//
// TimeoutErrorWithResponse MUST be called before returning from Handler
// if there are references to ctx and/or its members in other goroutines remain.
//
// Usage of this function is discouraged. Prefer eliminating ctx references
// from pending goroutines instead of using this function.
func (ctx *Ctx) TimeoutErrorWithResponse(resp *Response) {
	respCopy := &Response{}
	resp.CopyTo(respCopy)
	ctx.timeoutResponse = respCopy
}

// tcpKeepAliveListener sets TCP keep-alive timeouts on accepted
// connections. It's used by ListenAndServe, ListenAndServeTLS and
// ListenAndServeTLSEmbed so dead TCP connections (e.g. closing laptop mid-download)
// eventually go away.
type tcpKeepaliveListener struct {
	*net.TCPListener
	keepalive       bool
	keepalivePeriod time.Duration
}

func (ln tcpKeepaliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	if err := tc.SetKeepAlive(ln.keepalive); err != nil {
		_ = tc.Close()
		return nil, err
	}
	if ln.keepalivePeriod > 0 {
		if err := tc.SetKeepAlivePeriod(ln.keepalivePeriod); err != nil {
			_ = tc.Close()
			return nil, err
		}
	}
	return tc, nil
}

// ListenAndServe serves HTTP requests from the given TCP4 addr.
//
// Pass custom listener to Serve if you need listening on non-TCP4 media
// such as IPv6.
//
// Accepted connections are configured to enable TCP keep-alives.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	if tcpln, ok := ln.(*net.TCPListener); ok {
		return s.Serve(tcpKeepaliveListener{
			TCPListener:     tcpln,
			keepalive:       s.TCPKeepalive,
			keepalivePeriod: s.TCPKeepalivePeriod,
		})
	}
	return s.Serve(ln)
}

// DefaultConcurrency is the maximum number of concurrent connections
// the Server may serve by default (i.e. if Server.Concurrency isn't set).
const DefaultConcurrency = 256 * 1024

// Serve serves incoming connections from the given listener.
//
// Serve blocks until the given listener returns permanent error.
func (s *Server) Serve(ln net.Listener) error {
	var lastOverflowErrorTime time.Time
	var c net.Conn
	var err error

	maxWorkersCount := s.getConcurrency()

	s.mu.Lock()
	{
		s.ln = append(s.ln, ln)
		if s.done == nil {
			s.done = make(chan struct{})
		}

		if s.concurrencyCh == nil {
			s.concurrencyCh = make(chan struct{}, maxWorkersCount)
		}
	}
	s.mu.Unlock()

	wp := &workerPool{
		WorkerFunc:      s.serveConn,
		MaxWorkersCount: maxWorkersCount,
		LogAllErrors:    s.LogAllErrors,
		Logger:          s.logger(),
		connState:       s.setState,
	}
	wp.Start()

	// Count our waiting to accept a connection as an open connection.
	// This way we can't get into any weird state where just after accepting
	// a connection Shutdown is called which reads open as 0 because it isn't
	// incremented yet.
	atomic.AddInt32(&s.open, 1)
	defer atomic.AddInt32(&s.open, -1)

	for {
		if c, err = acceptConn(s, ln); err != nil {
			wp.Stop()
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.setState(c, StateNew)
		atomic.AddInt32(&s.open, 1)
		if !wp.Serve(c) {
			atomic.AddInt32(&s.open, -1)
			s.writeFastError(c, StatusServiceUnavailable,
				"The connection cannot be served because Server.Concurrency limit exceeded")
			c.Close()
			s.setState(c, StateClosed)
			if time.Since(lastOverflowErrorTime) > time.Minute {
				s.logger().Printf("The incoming connection cannot be served, because %d concurrent connections are served. "+
					"Try increasing Server.Concurrency", maxWorkersCount)
				lastOverflowErrorTime = time.Now()
			}

			// The current server reached concurrency limit,
			// so give other concurrently running servers a chance
			// accepting incoming connections on the same address.
			//
			// There is a hope other servers didn't reach their
			// concurrency limits yet :)
			//
			// See also: https://github.com/go-faster/hx/pull/485#discussion_r239994990
			if s.SleepWhenConcurrencyLimitsExceeded > 0 {
				time.Sleep(s.SleepWhenConcurrencyLimitsExceeded)
			}
		}
		c = nil
	}
}

// Shutdown gracefully shuts down the server without interrupting any active connections.
// Shutdown works by first closing all open listeners and then waiting indefinitely for all connections to return to idle and then shut down.
//
// When Shutdown is called, Serve, ListenAndServe, and ListenAndServeTLS immediately return nil.
// Make sure the program doesn't exit and waits instead for Shutdown to return.
//
// Shutdown does not close keepalive connections so its recommended to set ReadTimeout and IdleTimeout to something else than 0.
func (s *Server) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	atomic.StoreInt32(&s.stop, 1)
	defer atomic.StoreInt32(&s.stop, 0)

	if s.ln == nil {
		return nil
	}

	for _, ln := range s.ln {
		if err := ln.Close(); err != nil {
			return err
		}
	}

	if s.done != nil {
		close(s.done)
	}

	// Closing the listener will make Serve() call Stop on the worker pool.
	// Setting .stop to 1 will make serveConn() break out of its loop.
	// Now we just have to wait until all workers are done.
	for {
		if open := atomic.LoadInt32(&s.open); open == 0 {
			break
		}
		// This is not an optimal solution but using a sync.WaitGroup
		// here causes data races as it's hard to prevent Add() to be called
		// while Wait() is waiting.
		time.Sleep(time.Millisecond * 100)
	}

	s.done = nil
	s.ln = nil
	return nil
}

func acceptConn(s *Server, ln net.Listener) (net.Conn, error) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if c != nil {
				panic("BUG: net.Listener returned non-nil conn and non-nil error")
			}
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				s.logger().Printf("Temporary error when accepting new connections: %s", netErr)
				time.Sleep(time.Second)
				continue
			}
			if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger().Printf("Permanent error when accepting new connections: %s", err)
				return nil, err
			}
			return nil, io.EOF
		}
		if c == nil {
			panic("BUG: net.Listener returned (nil, nil)")
		}
		return c, nil
	}
}

var defaultLogger = Logger(log.New(os.Stderr, "", log.LstdFlags))

func (s *Server) logger() Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return defaultLogger
}

var (
	// ErrPerIPConnLimit may be returned from ServeConn if the number of connections
	// per ip exceeds Server.MaxConnsPerIP.
	ErrPerIPConnLimit = errors.New("too many connections per ip")

	// ErrConcurrencyLimit may be returned from ServeConn if the number
	// of concurrently served connections exceeds Server.Concurrency.
	ErrConcurrencyLimit = errors.New("cannot serve the connection because Server.Concurrency concurrent connections are served")
)

// ServeConn serves HTTP requests from the given connection.
//
// ServeConn returns nil if all requests from the c are successfully served.
// It returns non-nil error otherwise.
//
// Connection c must immediately propagate all the data passed to Write()
// to the client. Otherwise requests' processing may hang.
//
// ServeConn closes c before returning.
func (s *Server) ServeConn(c net.Conn) error {
	n := atomic.AddUint32(&s.concurrency, 1)
	if n > uint32(s.getConcurrency()) {
		atomic.AddUint32(&s.concurrency, ^uint32(0))
		s.writeFastError(c, StatusServiceUnavailable, "The connection cannot be served because Server.Concurrency limit exceeded")
		c.Close()
		return ErrConcurrencyLimit
	}

	atomic.AddInt32(&s.open, 1)

	err := s.serveConn(c)

	atomic.AddUint32(&s.concurrency, ^uint32(0))

	if err != errHijacked {
		err1 := c.Close()
		s.setState(c, StateClosed)
		if err == nil {
			err = err1
		}
	} else {
		err = nil
		s.setState(c, StateHijacked)
	}
	return err
}

var errHijacked = errors.New("connection has been hijacked")

// GetCurrentConcurrency returns a number of currently served
// connections.
//
// This function is intended be used by monitoring systems
func (s *Server) GetCurrentConcurrency() uint32 {
	return atomic.LoadUint32(&s.concurrency)
}

// GetOpenConnectionsCount returns a number of opened connections.
//
// This function is intended be used by monitoring systems
func (s *Server) GetOpenConnectionsCount() int32 {
	if atomic.LoadInt32(&s.stop) == 0 {
		// Decrement by one to avoid reporting the extra open value that gets
		// counted while the server is listening.
		return atomic.LoadInt32(&s.open) - 1
	}
	// This is not perfect, because s.stop could have changed to zero
	// before we load the value of s.open. However, in the common case
	// this avoids underreporting open connections by 1 during server shutdown.
	return atomic.LoadInt32(&s.open)
}

func (s *Server) getConcurrency() int {
	n := s.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	return n
}

var globalConnID uint64

func nextConnID() uint64 {
	return atomic.AddUint64(&globalConnID, 1)
}

// DefaultMaxRequestBodySize is the maximum request body size the server
// reads by default.
//
// See Server.MaxRequestBodySize for details.
const DefaultMaxRequestBodySize = 4 * 1024 * 1024

func (s *Server) idleTimeout() time.Duration {
	if s.IdleTimeout != 0 {
		return s.IdleTimeout
	}
	return s.ReadTimeout
}

func (s *Server) serveConnCleanup() {
	atomic.AddInt32(&s.open, -1)
	atomic.AddUint32(&s.concurrency, ^uint32(0))
}

func (s *Server) serveConn(c net.Conn) (err error) {
	defer s.serveConnCleanup()
	atomic.AddUint32(&s.concurrency, 1)

	var serverName []byte
	if !s.NoDefaultServerHeader {
		serverName = s.getServerName()
	}
	connRequestNum := uint64(0)
	connID := nextConnID()
	connTime := time.Now()
	maxRequestBodySize := s.MaxRequestBodySize
	if maxRequestBodySize <= 0 {
		maxRequestBodySize = DefaultMaxRequestBodySize
	}
	writeTimeout := s.WriteTimeout
	previousWriteTimeout := time.Duration(0)

	ctx := s.acquireCtx(c)
	ctx.connTime = connTime
	var (
		br *bufio.Reader
		bw *bufio.Writer

		timeoutResponse *Response

		connectionClose bool
		isHTTP11        bool

		reqReset               bool
		continueReadingRequest = true
	)
	for {
		connRequestNum++

		// If this is a keep-alive connection set the idle timeout.
		if connRequestNum > 1 {
			if d := s.idleTimeout(); d > 0 {
				if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
					panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", d, err))
				}
			}
		}

		if br != nil {
			if br == nil {
				br = acquireReader(ctx)
			}

			// If this is a keep-alive connection we want to try and read the first bytes
			// within the idle time.
			if connRequestNum > 1 {
				var b []byte
				b, err = br.Peek(1)
				if len(b) == 0 {
					// If reading from a keep-alive connection returns nothing it means
					// the connection was closed (either timeout or from the other side).
					if err != io.EOF {
						err = ErrNothingRead{err}
					}
				}
			}
		} else {
			// If this is a keep-alive connection acquireByteReader will try to peek
			// a couple of bytes already so the idle timeout will already be used.
			br, err = acquireByteReader(&ctx)
		}

		ctx.Response.Header.noDefaultContentType = s.NoDefaultContentType
		ctx.Response.Header.noDefaultDate = s.NoDefaultDate

		if err == nil {
			if s.ReadTimeout > 0 {
				if err := c.SetReadDeadline(time.Now().Add(s.ReadTimeout)); err != nil {
					panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", s.ReadTimeout, err))
				}
			} else if s.IdleTimeout > 0 && connRequestNum > 1 {
				// If this was an idle connection and the server has an IdleTimeout but
				// no ReadTimeout then we should remove the ReadTimeout.
				if err := c.SetReadDeadline(zeroTime); err != nil {
					panic(fmt.Sprintf("BUG: error in SetReadDeadline(zeroTime): %s", err))
				}
			}
			if s.DisableHeaderNamesNormalizing {
				ctx.Request.Header.DisableNormalizing()
				ctx.Response.Header.DisableNormalizing()
			}

			// Reading Headers.
			//
			// If we have pipline response in the outgoing buffer,
			// we only want to try and read the next headers once.
			// If we have to wait for the next request we flush the
			// outgoing buffer first so it doesn't have to wait.
			if bw != nil && bw.Buffered() > 0 {
				err = ctx.Request.Header.readLoop(br, false)
				if err == errNeedMore {
					err = bw.Flush()
					if err != nil {
						break
					}

					err = ctx.Request.Header.Read(br)
				}
			} else {
				err = ctx.Request.Header.Read(br)
			}

			if err == nil {
				if onHdrRecv := s.HeaderReceived; onHdrRecv != nil {
					reqConf := onHdrRecv(&ctx.Request.Header)
					if reqConf.ReadTimeout > 0 {
						deadline := time.Now().Add(reqConf.ReadTimeout)
						if err := c.SetReadDeadline(deadline); err != nil {
							panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", deadline, err))
						}
					}
					if reqConf.MaxRequestBodySize > 0 {
						maxRequestBodySize = reqConf.MaxRequestBodySize
					}
					if reqConf.WriteTimeout > 0 {
						writeTimeout = reqConf.WriteTimeout
					}
				}
				err = ctx.Request.readBody(br)
			}

			if err == nil {
				// If we read any bytes off the wire, we're active.
				s.setState(c, StateActive)
			}
		}

		if err != nil {
			if err == io.EOF {
				err = nil
			} else if nr, ok := err.(ErrNothingRead); ok {
				if connRequestNum > 1 {
					// This is not the first request and we haven't read a single byte
					// of a new request yet. This means it's just a keep-alive connection
					// closing down either because the remote closed it or because
					// or a read timeout on our side. Either way just close the connection
					// and don't return any error response.
					err = nil
				} else {
					err = nr.error
				}
			}

			if err != nil {
				bw = s.writeErrorResponse(bw, ctx, serverName, err)
			}
			break
		}

		// 'Expect: 100-continue' request handling.
		// See https://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html#sec8.2.3 for details.
		if ctx.Request.MayContinue() {
			// Allow the ability to deny reading the incoming request body
			if s.ContinueHandler != nil {
				if continueReadingRequest = s.ContinueHandler(&ctx.Request.Header); !continueReadingRequest {
					if br != nil {
						br.Reset(ctx.c)
					}

					ctx.SetStatusCode(StatusExpectationFailed)
				}
			}

			if continueReadingRequest {
				if bw == nil {
					bw = acquireWriter(ctx)
				}

				// Send 'HTTP/1.1 100 Continue' response.
				_, err = bw.Write(strResponseContinue)
				if err != nil {
					break
				}
				err = bw.Flush()
				if err != nil {
					break
				}

				// Read request body.
				if br == nil {
					br = acquireReader(ctx)
				}

				if err := ctx.Request.ContinueReadBody(br); err != nil {
					bw = s.writeErrorResponse(bw, ctx, serverName, err)
					break
				}
			}
		}

		connectionClose = ctx.Request.Header.ConnectionClose()
		isHTTP11 = ctx.Request.Header.IsHTTP11()

		if serverName != nil {
			ctx.Response.Header.SetServerBytes(serverName)
		}
		ctx.connID = connID
		ctx.connRequestNum = connRequestNum
		ctx.time = time.Now()

		// If a client denies a request the handler should not be called
		if continueReadingRequest {
			s.Handler(ctx)
		}

		timeoutResponse = ctx.timeoutResponse
		if timeoutResponse != nil {
			// Acquire a new ctx because the old one will still be in use by the timeout out handler.
			ctx = s.acquireCtx(c)
			timeoutResponse.CopyTo(&ctx.Response)
		}

		if !ctx.IsGet() && ctx.IsHead() {
			ctx.Response.SkipBody = true
		}
		reqReset = true
		ctx.Request.Reset()

		if writeTimeout > 0 {
			if err := c.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
				panic(fmt.Sprintf("BUG: error in SetWriteDeadline(%s): %s", writeTimeout, err))
			}
			previousWriteTimeout = writeTimeout
		} else if previousWriteTimeout > 0 {
			// We don't want a write timeout but we previously set one, remove it.
			if err := c.SetWriteDeadline(zeroTime); err != nil {
				panic(fmt.Sprintf("BUG: error in SetWriteDeadline(zeroTime): %s", err))
			}
			previousWriteTimeout = 0
		}

		connectionClose = connectionClose || ctx.Response.ConnectionClose() || (s.CloseOnShutdown && atomic.LoadInt32(&s.stop) == 1)
		if connectionClose {
			ctx.Response.Header.SetCanonical(strConnection, strClose)
		} else if !isHTTP11 {
			// Set 'Connection: keep-alive' response header for non-HTTP/1.1 request.
			// There is no need in setting this header for http/1.1, since in http/1.1
			// connections are keep-alive by default.
			ctx.Response.Header.SetCanonical(strConnection, strKeepAlive)
		}

		if serverName != nil && len(ctx.Response.Header.Server()) == 0 {
			ctx.Response.Header.SetServerBytes(serverName)
		}

		s.setState(c, StateIdle)

		if atomic.LoadInt32(&s.stop) == 1 {
			err = nil
			break
		}
	}

	if br != nil {
		releaseReader(s, br)
	}
	if bw != nil {
		releaseWriter(s, bw)
	}
	if ctx != nil {
		// in unexpected cases the for loop will break
		// before request reset call. in such cases, call it before
		// release to fix #548
		if !reqReset {
			ctx.Request.Reset()
		}
		s.releaseCtx(ctx)
	}
	return
}

func (s *Server) setState(nc net.Conn, state ConnState) {
	if hook := s.ConnState; hook != nil {
		hook(nc, state)
	}
}

// LastTimeoutErrorResponse returns the last timeout response set
// via TimeoutError* call.
//
// This function is intended for custom server implementations.
func (ctx *Ctx) LastTimeoutErrorResponse() *Response {
	return ctx.timeoutResponse
}

func writeResponse(ctx *Ctx, w *bufio.Writer) error {
	if ctx.timeoutResponse != nil {
		panic("BUG: cannot write timed out response")
	}
	err := ctx.Response.Write(w)
	ctx.Response.Reset()
	return err
}

const (
	defaultReadBufferSize  = 4096
	defaultWriteBufferSize = 4096
)

func acquireByteReader(ctxP **Ctx) (*bufio.Reader, error) {
	ctx := *ctxP
	s := ctx.s
	c := ctx.c
	s.releaseCtx(ctx)

	// Make GC happy, so it could garbage collect ctx
	// while we waiting for the next request.
	ctx = nil
	*ctxP = nil

	var b [1]byte
	n, err := c.Read(b[:])

	ctx = s.acquireCtx(c)
	*ctxP = ctx
	if err != nil {
		// Treat all errors as EOF on unsuccessful read
		// of the first request byte.
		return nil, io.EOF
	}
	if n != 1 {
		panic("BUG: Reader must return at least one byte")
	}

	ctx.fbr.c = c
	ctx.fbr.ch = b[0]
	ctx.fbr.byteRead = false
	r := acquireReader(ctx)
	r.Reset(&ctx.fbr)
	return r, nil
}

func acquireReader(ctx *Ctx) *bufio.Reader {
	v := ctx.s.readerPool.Get()
	if v == nil {
		n := ctx.s.ReadBufferSize
		if n <= 0 {
			n = defaultReadBufferSize
		}
		return bufio.NewReaderSize(ctx.c, n)
	}
	r := v.(*bufio.Reader)
	r.Reset(ctx.c)
	return r
}

func releaseReader(s *Server, r *bufio.Reader) {
	s.readerPool.Put(r)
}

func acquireWriter(ctx *Ctx) *bufio.Writer {
	v := ctx.s.writerPool.Get()
	if v == nil {
		n := ctx.s.WriteBufferSize
		if n <= 0 {
			n = defaultWriteBufferSize
		}
		return bufio.NewWriterSize(ctx.c, n)
	}
	w := v.(*bufio.Writer)
	w.Reset(ctx.c)
	return w
}

func releaseWriter(s *Server, w *bufio.Writer) {
	s.writerPool.Put(w)
}

func (s *Server) acquireCtx(c net.Conn) (ctx *Ctx) {
	v := s.ctxPool.Get()
	if v == nil {
		ctx = &Ctx{
			s: s,
		}
		ctx.Request.keepBodyBuffer = true
		ctx.Response.keepBodyBuffer = true
	} else {
		ctx = v.(*Ctx)
	}
	ctx.c = c
	return
}

// Init2 prepares ctx for passing to Handler.
//
// conn is used only for determining local and remote addresses.
//
// This function is intended for custom Server implementations.
// See https://github.com/valyala/httpteleport for details.
func (ctx *Ctx) Init2(conn net.Conn, logger Logger, reduceMemoryUsage bool) {
	ctx.c = conn
	ctx.remoteAddr = nil
	ctx.logger.logger = logger
	ctx.connID = nextConnID()
	ctx.s = fakeServer
	ctx.connRequestNum = 0
	ctx.connTime = time.Now()

	keepBodyBuffer := !reduceMemoryUsage
	ctx.Request.keepBodyBuffer = keepBodyBuffer
	ctx.Response.keepBodyBuffer = keepBodyBuffer
}

// Init prepares ctx for passing to Handler.
//
// remoteAddr and logger are optional. They are used by Ctx.Logger().
//
// This function is intended for custom Server implementations.
func (ctx *Ctx) Init(req *Request, remoteAddr net.Addr, logger Logger) {
	if remoteAddr == nil {
		remoteAddr = zeroTCPAddr
	}
	c := &fakeAddrer{
		laddr: zeroTCPAddr,
		raddr: remoteAddr,
	}
	if logger == nil {
		logger = defaultLogger
	}
	ctx.Init2(c, logger, true)
	req.CopyTo(&ctx.Request)
}

// Deadline returns the time when work done on behalf of this context
// should be canceled. Deadline returns ok==false when no deadline is
// set. Successive calls to Deadline return the same results.
//
// This method always returns 0, false and is only present to make
// Ctx implement the context interface.
func (ctx *Ctx) Deadline() (deadline time.Time, ok bool) {
	return
}

// Done returns a channel that's closed when work done on behalf of this
// context should be canceled. Done may return nil if this context can
// never be canceled. Successive calls to Done return the same value.
func (ctx *Ctx) Done() <-chan struct{} {
	return ctx.s.done
}

// Err returns a non-nil error value after Done is closed,
// successive calls to Err return the same error.
// If Done is not yet closed, Err returns nil.
// If Done is closed, Err returns a non-nil error explaining why:
// Canceled if the context was canceled (via server Shutdown)
// or DeadlineExceeded if the context's deadline passed.
func (ctx *Ctx) Err() error {
	select {
	case <-ctx.s.done:
		return context.Canceled
	default:
		return nil
	}
}

var fakeServer = &Server{
	// Initialize concurrencyCh for TimeoutHandler
	concurrencyCh: make(chan struct{}, DefaultConcurrency),
}

type fakeAddrer struct {
	net.Conn
	laddr net.Addr
	raddr net.Addr
}

func (fa *fakeAddrer) RemoteAddr() net.Addr {
	return fa.raddr
}

func (fa *fakeAddrer) LocalAddr() net.Addr {
	return fa.laddr
}

func (fa *fakeAddrer) Read(p []byte) (int, error) {
	panic("BUG: unexpected Read call")
}

func (fa *fakeAddrer) Write(p []byte) (int, error) {
	panic("BUG: unexpected Write call")
}

func (fa *fakeAddrer) Close() error {
	panic("BUG: unexpected Close call")
}

func (s *Server) releaseCtx(ctx *Ctx) {
	if ctx.timeoutResponse != nil {
		panic("BUG: cannot release timed out Ctx")
	}
	ctx.c = nil
	ctx.remoteAddr = nil
	ctx.fbr.c = nil
	s.ctxPool.Put(ctx)
}

func (s *Server) getServerName() []byte {
	v := s.serverName.Load()
	var serverName []byte
	if v == nil {
		serverName = []byte(s.Name)
		if len(serverName) == 0 {
			serverName = defaultServerName
		}
		s.serverName.Store(serverName)
	} else {
		serverName = v.([]byte)
	}
	return serverName
}

func (s *Server) writeFastError(w io.Writer, statusCode int, msg string) {
	w.Write(formatStatusLine(nil, strHTTP11, statusCode, s2b(StatusMessage(statusCode)))) //nolint:errcheck

	server := ""
	if !s.NoDefaultServerHeader {
		server = fmt.Sprintf("Server: %s\r\n", s.getServerName())
	}

	date := ""
	if !s.NoDefaultDate {
		serverDateOnce.Do(updateServerDate)
		date = fmt.Sprintf("Date: %s\r\n", serverDate.Load())
	}

	fmt.Fprintf(w, "Connection: close\r\n"+
		server+
		date+
		"Content-Type: text/plain\r\n"+
		"Content-Length: %d\r\n"+
		"\r\n"+
		"%s",
		len(msg), msg)
}

func defaultErrorHandler(ctx *Ctx, err error) {
	if _, ok := err.(*ErrSmallBuffer); ok {
		ctx.Error("Too big request header", StatusRequestHeaderFieldsTooLarge)
	} else if netErr, ok := err.(*net.OpError); ok && netErr.Timeout() {
		ctx.Error("Request timeout", StatusRequestTimeout)
	} else {
		ctx.Error("Error when parsing request", StatusBadRequest)
	}
}

func (s *Server) writeErrorResponse(bw *bufio.Writer, ctx *Ctx, serverName []byte, err error) *bufio.Writer {
	errorHandler := defaultErrorHandler
	if s.ErrorHandler != nil {
		errorHandler = s.ErrorHandler
	}

	errorHandler(ctx, err)

	if serverName != nil {
		ctx.Response.Header.SetServerBytes(serverName)
	}
	ctx.SetConnectionClose()
	if bw == nil {
		bw = acquireWriter(ctx)
	}
	writeResponse(ctx, bw) //nolint:errcheck
	bw.Flush()
	return bw
}

// A ConnState represents the state of a client connection to a server.
// It's used by the optional Server.ConnState hook.
type ConnState int

const (
	// StateNew represents a new connection that is expected to
	// send a request immediately. Connections begin at this
	// state and then transition to either StateActive or
	// StateClosed.
	StateNew ConnState = iota

	// StateActive represents a connection that has read 1 or more
	// bytes of a request. The Server.ConnState hook for
	// StateActive fires before the request has entered a handler
	// and doesn't fire again until the request has been
	// handled. After the request is handled, the state
	// transitions to StateClosed, StateHijacked, or StateIdle.
	// For HTTP/2, StateActive fires on the transition from zero
	// to one active request, and only transitions away once all
	// active requests are complete. That means that ConnState
	// cannot be used to do per-request work; ConnState only notes
	// the overall state of the connection.
	StateActive

	// StateIdle represents a connection that has finished
	// handling a request and is in the keep-alive state, waiting
	// for a new request. Connections transition from StateIdle
	// to either StateActive or StateClosed.
	StateIdle

	// StateHijacked represents a hijacked connection.
	// This is a terminal state. It does not transition to StateClosed.
	StateHijacked

	// StateClosed represents a closed connection.
	// This is a terminal state. Hijacked connections do not
	// transition to StateClosed.
	StateClosed
)

var stateName = map[ConnState]string{
	StateNew:      "new",
	StateActive:   "active",
	StateIdle:     "idle",
	StateHijacked: "hijacked",
	StateClosed:   "closed",
}

func (c ConnState) String() string {
	return stateName[c]
}
