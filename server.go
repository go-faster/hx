package hx

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

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
	// DefaultWorkers is used if not set.
	//
	// Workers only works if you either call Serve once, or only ServeConn multiple times.
	// It works with ListenAndServe as well.
	Workers int

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

	// CloseOnShutdown when true adds a `Connection: close` header when the server is shutting down.
	CloseOnShutdown bool

	// ConnState specifies an optional callback function that is
	// called when a client connection changes state. See the
	// ConnState type and associated constants for details.
	ConnState func(net.Conn, ConnState)

	// Logger, which is used by Ctx.Logger().
	//
	// Nop by default.
	Logger *zap.Logger

	serverName atomic.Value

	ctxPool    sync.Pool
	readerPool sync.Pool
	writerPool sync.Pool

	// listeners to close in Shutdown()
	ln []net.Listener

	mu   sync.Mutex
	open int32
	stop int32
	done chan struct{}
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

	lg   *zap.Logger
	done <-chan struct{}
	c    net.Conn
	fbr  firstByteReader
}

// Value is no-op implementation for context.Context.
func (c *Ctx) Value(key interface{}) interface{} {
	return nil
}

// Conn returns a reference to the underlying net.Conn.
//
// WARNING: Only use this method if you know what you are doing!
//
// Reading from or writing to the returned connection will end badly!
func (c *Ctx) Conn() net.Conn {
	return c.c
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

var zeroTCPAddr = &net.TCPAddr{
	IP: net.IPv4zero,
}

// String returns unique string representation of the ctx.
//
// The returned value may be useful for logging.
func (c *Ctx) String() string {
	return fmt.Sprintf("#%016X - %s<->%s - %s %s", c.ID(), c.LocalAddr(), c.RemoteAddr(), c.Request.Header.Method(), c.URI().FullURI())
}

// ID returns unique ID of the request.
func (c *Ctx) ID() uint64 {
	return (c.connID << 32) | c.connRequestNum
}

// ConnID returns unique connection ID.
//
// This ID may be used to match distinct requests to the same incoming
// connection.
func (c *Ctx) ConnID() uint64 {
	return c.connID
}

// Time returns Handler call time.
func (c *Ctx) Time() time.Time {
	return c.time
}

// ConnTime returns the time the server started serving the connection
// the current request came from.
func (c *Ctx) ConnTime() time.Time {
	return c.connTime
}

// ConnRequestNum returns request sequence number
// for the current connection.
//
// Sequence starts with 1.
func (c *Ctx) ConnRequestNum() uint64 {
	return c.connRequestNum
}

// SetConnectionClose sets 'Connection: close' response header and closes
// connection after the Handler returns.
func (c *Ctx) SetConnectionClose() {
	c.Response.SetConnectionClose()
}

// SetStatusCode sets response status code.
func (c *Ctx) SetStatusCode(statusCode int) {
	c.Response.SetStatusCode(statusCode)
}

// SetContentType sets response Content-Type.
func (c *Ctx) SetContentType(contentType string) {
	c.Response.Header.SetContentType(contentType)
}

// SetContentTypeBytes sets response Content-Type.
//
// It is safe modifying contentType buffer after function return.
func (c *Ctx) SetContentTypeBytes(contentType []byte) {
	c.Response.Header.SetContentTypeBytes(contentType)
}

// RequestURI returns RequestURI.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) RequestURI() []byte {
	return c.Request.Header.RequestURI()
}

// URI returns requested uri.
//
// This uri is valid until your request handler returns.
func (c *Ctx) URI() *URI {
	return c.Request.URI()
}

// Referer returns request referer.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) Referer() []byte {
	return c.Request.Header.Referer()
}

// UserAgent returns User-Agent header value from the request.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) UserAgent() []byte {
	return c.Request.Header.UserAgent()
}

// Path returns requested path.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) Path() []byte {
	return c.URI().Path()
}

// Host returns requested host.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) Host() []byte {
	return c.URI().Host()
}

// QueryArgs returns query arguments from RequestURI.
//
// It doesn't return POST'ed arguments - use PostArgs() for this.
//
// See also PostArgs, FormValue and FormFile.
//
// These args are valid until your request handler returns.
func (c *Ctx) QueryArgs() *Args {
	return c.URI().QueryArgs()
}

// PostArgs returns POST arguments.
//
// It doesn't return query arguments from RequestURI - use QueryArgs for this.
//
// See also QueryArgs, FormValue and FormFile.
//
// These args are valid until your request handler returns.
func (c *Ctx) PostArgs() *Args {
	return c.Request.PostArgs()
}

// IsGet returns true if request method is GET.
func (c *Ctx) IsGet() bool {
	return c.Request.Header.IsGet()
}

// IsPost returns true if request method is POST.
func (c *Ctx) IsPost() bool {
	return c.Request.Header.IsPost()
}

// IsPut returns true if request method is PUT.
func (c *Ctx) IsPut() bool {
	return c.Request.Header.IsPut()
}

// IsDelete returns true if request method is DELETE.
func (c *Ctx) IsDelete() bool {
	return c.Request.Header.IsDelete()
}

// IsConnect returns true if request method is CONNECT.
func (c *Ctx) IsConnect() bool {
	return c.Request.Header.IsConnect()
}

// IsOptions returns true if request method is OPTIONS.
func (c *Ctx) IsOptions() bool {
	return c.Request.Header.IsOptions()
}

// IsTrace returns true if request method is TRACE.
func (c *Ctx) IsTrace() bool {
	return c.Request.Header.IsTrace()
}

// IsPatch returns true if request method is PATCH.
func (c *Ctx) IsPatch() bool {
	return c.Request.Header.IsPatch()
}

// Method return request method.
//
// Returned value is valid until your request handler returns.
func (c *Ctx) Method() []byte {
	return c.Request.Header.Method()
}

// IsHead returns true if request method is HEAD.
func (c *Ctx) IsHead() bool {
	return c.Request.Header.IsHead()
}

// RemoteAddr returns client address for the given request.
//
// Always returns non-nil result.
func (c *Ctx) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	if c.c == nil {
		return zeroTCPAddr
	}
	addr := c.c.RemoteAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// SetRemoteAddr sets remote address to the given value.
//
// Set nil value to resore default behaviour for using
// connection remote address.
func (c *Ctx) SetRemoteAddr(remoteAddr net.Addr) {
	c.remoteAddr = remoteAddr
}

// LocalAddr returns server address for the given request.
//
// Always returns non-nil result.
func (c *Ctx) LocalAddr() net.Addr {
	if c.c == nil {
		return zeroTCPAddr
	}
	addr := c.c.LocalAddr()
	if addr == nil {
		return zeroTCPAddr
	}
	return addr
}

// RemoteIP returns the client ip the request came from.
//
// Always returns non-nil result.
func (c *Ctx) RemoteIP() net.IP {
	return addrToIP(c.RemoteAddr())
}

// LocalIP returns the server ip the request came to.
//
// Always returns non-nil result.
func (c *Ctx) LocalIP() net.IP {
	return addrToIP(c.LocalAddr())
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
func (c *Ctx) Error(msg string, statusCode int) {
	c.Response.Reset()
	c.SetStatusCode(statusCode)
	c.SetContentTypeBytes(defaultContentType)
	c.SetBodyString(msg)
}

// Success sets response Content-Type and body to the given values.
func (c *Ctx) Success(contentType string, body []byte) {
	c.SetContentType(contentType)
	c.SetBody(body)
}

// SuccessString sets response Content-Type and body to the given values.
func (c *Ctx) SuccessString(contentType, body string) {
	c.SetContentType(contentType)
	c.SetBodyString(body)
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
func (c *Ctx) Redirect(uri string, statusCode int) {
	u := AcquireURI()
	c.URI().CopyTo(u)
	u.Update(uri)
	c.redirect(u.FullURI(), statusCode)
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
func (c *Ctx) RedirectBytes(uri []byte, statusCode int) {
	s := b2s(uri)
	c.Redirect(s, statusCode)
}

func (c *Ctx) redirect(uri []byte, statusCode int) {
	c.Response.Header.SetCanonical(strLocation, uri)
	statusCode = getRedirectStatusCode(statusCode)
	c.Response.SetStatusCode(statusCode)
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
func (c *Ctx) SetBody(body []byte) {
	c.Response.SetBody(body)
}

// SetBodyString sets response body to the given value.
func (c *Ctx) SetBodyString(body string) {
	c.Response.SetBodyString(body)
}

// ResetBody resets response body contents.
func (c *Ctx) ResetBody() {
	c.Response.ResetBody()
}

// IfModifiedSince returns true if lastModified exceeds 'If-Modified-Since'
// value from the request header.
//
// The function returns true also 'If-Modified-Since' request header is missing.
func (c *Ctx) IfModifiedSince(lastModified time.Time) bool {
	ifModStr := c.Request.Header.peek(strIfModifiedSince)
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
func (c *Ctx) NotModified() {
	c.Response.Reset()
	c.SetStatusCode(StatusNotModified)
}

// NotFound resets response and sets '404 Not Found' response status code.
func (c *Ctx) NotFound() {
	c.Response.Reset()
	c.SetStatusCode(StatusNotFound)
	c.SetBodyString("404 Page not found")
}

// Write writes p into response body.
func (c *Ctx) Write(p []byte) (int, error) {
	c.Response.AppendBody(p)
	return len(p), nil
}

// WriteString appends s to response body.
func (c *Ctx) WriteString(s string) (int, error) {
	c.Response.AppendBodyString(s)
	return len(s), nil
}

// PostBody returns POST request body.
//
// The returned bytes are valid until your request handler returns.
func (c *Ctx) PostBody() []byte {
	return c.Request.Body()
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
func (c *Ctx) Logger() *zap.Logger {
	return c.lg
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
		return s.Serve(context.TODO(), tcpKeepaliveListener{
			TCPListener:     tcpln,
			keepalive:       s.TCPKeepalive,
			keepalivePeriod: s.TCPKeepalivePeriod,
		})
	}
	return s.Serve(context.TODO(), ln)
}

// DefaultWorkers is the maximum number of concurrent connections
// the Server may serve by default (i.e. if Server.Workers isn't set).
const DefaultWorkers = 100

// Serve serves incoming connections from the given listener.
//
// Serve blocks until the given listener returns permanent error.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	workers := s.getWorkers()

	s.mu.Lock()
	{
		s.ln = append(s.ln, ln)
		if s.done == nil {
			s.done = make(chan struct{})
		}
	}
	s.mu.Unlock()

	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-ctx.Done()
		close(s.done)
		return nil
	})

	for i := 0; i < workers; i++ {
		g.Go(func() error {
			var c net.Conn
			var err error

			for {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if c, err = s.accept(ln); err != nil {
					if err == io.EOF {
						return nil
					}
					return err
				}
				s.setState(c, StateNew)
				atomic.AddInt32(&s.open, 1)
				err = s.serveConn(c)
				closeErr := c.Close()
				s.setState(c, StateClosed)
				if err == nil {
					err = closeErr
				}
				if err != nil {
					s.logger().Error("serve", zap.Error(err))
				}
				c = nil
			}
		})
	}

	return g.Wait()
}

func (s *Server) accept(ln net.Listener) (net.Conn, error) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if c != nil {
				panic("BUG: net.Listener returned non-nil conn and non-nil error")
			}
			if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
				s.logger().Error("temporary error while accepting new connections",
					zap.Error(netErr),
				)
				time.Sleep(time.Second)
				continue
			}
			if err != io.EOF && !strings.Contains(err.Error(), "use of closed network connection") {
				s.logger().Error("permanent error when accepting new connections",
					zap.Error(err),
				)
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

var nopLogger = zap.NewNop()

func (s *Server) logger() *zap.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return nopLogger
}

// ServeConn serves HTTP requests from the given connection.
//
// ServeConn returns nil if all requests from the c are successfully served.
// It returns non-nil error otherwise.
//
// Connection c must immediately propagate all the data passed to Write()
// to the client. Otherwise, requests' processing may hang.
//
// ServeConn closes c before returning.
func (s *Server) ServeConn(c net.Conn) error {
	atomic.AddInt32(&s.open, 1)

	err := s.serveConn(c)

	closeErr := c.Close()
	s.setState(c, StateClosed)
	if err == nil {
		err = closeErr
	}

	return err
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

func (s *Server) getWorkers() int {
	if s.Workers > 0 {
		return s.Workers
	}
	return DefaultWorkers
}

var globalConnID uint64

func nextConnID() uint64 {
	return atomic.AddUint64(&globalConnID, 1)
}

func (s *Server) idleTimeout() time.Duration {
	if s.IdleTimeout != 0 {
		return s.IdleTimeout
	}
	return s.ReadTimeout
}

func (s *Server) serveConn(c net.Conn) (err error) {
	var serverName []byte
	if !s.NoDefaultServerHeader {
		serverName = s.getServerName()
	}
	connRequestNum := uint64(0)
	connID := nextConnID()
	connTime := time.Now()
	writeTimeout := s.WriteTimeout
	previousWriteTimeout := time.Duration(0)

	ctx := s.acquireCtx(c)
	ctx.connTime = connTime
	var (
		br *bufio.Reader
		bw *bufio.Writer

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
			br, err = s.acquireByteReader(&ctx)
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
					bw = s.acquireWriter(ctx)
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
					br = s.acquireReader(ctx)
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
		if ctx.IsHead() {
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

		if bw == nil {
			bw = s.acquireWriter(ctx)
		}
		if err = writeResponse(ctx, bw); err != nil {
			break
		}

		// Only flush the writer if we don't have another request in the pipeline.
		// This is a big of an ugly optimization for https://www.techempower.com/benchmarks/
		// This benchmark will send 16 pipelined requests. It is faster to pack as many responses
		// in a TCP packet and send it back at once than waiting for a flush every request.
		// In real world circumstances this behaviour could be argued as being wrong.
		if br == nil || br.Buffered() == 0 || connectionClose {
			err = bw.Flush()
			if err != nil {
				break
			}
		}
		if connectionClose {
			break
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

func writeResponse(ctx *Ctx, w *bufio.Writer) error {
	err := ctx.Response.Write(w)
	ctx.Response.Reset()
	return err
}

const (
	defaultReadBufferSize  = 4096
	defaultWriteBufferSize = 4096
)

func (s *Server) acquireByteReader(ctxP **Ctx) (*bufio.Reader, error) {
	ctx := *ctxP
	c := ctx.c
	s.releaseCtx(ctx)

	// Make GC happy, so it could collect ctx
	// while we are waiting for the next request.
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
	r := s.acquireReader(ctx)
	r.Reset(&ctx.fbr)
	return r, nil
}

func (s *Server) acquireReader(ctx *Ctx) *bufio.Reader {
	v := s.readerPool.Get()
	if v == nil {
		n := s.ReadBufferSize
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

func (s *Server) acquireWriter(ctx *Ctx) *bufio.Writer {
	v := s.writerPool.Get()
	if v == nil {
		n := s.WriteBufferSize
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
		ctx = &Ctx{}
		ctx.Request.keepBodyBuffer = true
		ctx.Response.keepBodyBuffer = true
	} else {
		ctx = v.(*Ctx)
	}
	ctx.c = c
	return
}

// Deadline returns the time when work done on behalf of this context
// should be canceled. Deadline returns ok==false when no deadline is
// set. Successive calls to Deadline return the same results.
//
// This method always returns 0, false and is only present to make
// Ctx implement the context interface.
func (c *Ctx) Deadline() (deadline time.Time, ok bool) {
	return
}

// Done returns a channel that's closed when work done on behalf of this
// context should be canceled. Done may return nil if this context can
// never be canceled. Successive calls to Done return the same value.
func (c *Ctx) Done() <-chan struct{} { return c.done }

// Err returns a non-nil error value after Done is closed,
// successive calls to Err return the same error.
// If Done is not yet closed, Err returns nil.
// If Done is closed, Err returns a non-nil error explaining why:
// Canceled if the context was canceled (via server Shutdown)
// or DeadlineExceeded if the context's deadline passed.
func (c *Ctx) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (s *Server) releaseCtx(ctx *Ctx) {
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
		bw = s.acquireWriter(ctx)
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

	// StateClosed represents a closed connection.
	// This is a terminal state. Hijacked connections do not
	// transition to StateClosed.
	StateClosed
)

var stateName = map[ConnState]string{
	StateNew:    "new",
	StateActive: "active",
	StateIdle:   "idle",
	StateClosed: "closed",
}

func (c ConnState) String() string {
	return stateName[c]
}
