package hx

import (
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

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
// Set nil value to resore default behavior for using
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
//   - StatusMovedPermanently (301)
//   - StatusFound (302)
//   - StatusSeeOther (303)
//   - StatusTemporaryRedirect (307)
//   - StatusPermanentRedirect (308)
//
// All other statusCode values are replaced by StatusFound (302).
//
// The redirect uri may be either absolute or relative to the current
// request uri. Fasthttp will always send an absolute uri back to the client.
// To send a relative uri you can use the following code:
//
//	strLocation = []byte("Location") // Put this with your top level var () declarations.
//	ctx.Response.Header.SetCanonical(strLocation, "/relative?uri")
//	ctx.Response.SetStatusCode(fasthttp.StatusMovedPermanently)
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
//   - StatusMovedPermanently (301)
//   - StatusFound (302)
//   - StatusSeeOther (303)
//   - StatusTemporaryRedirect (307)
//   - StatusPermanentRedirect (308)
//
// All other statusCode values are replaced by StatusFound (302).
//
// The redirect uri may be either absolute or relative to the current
// request uri. Fasthttp will always send an absolute uri back to the client.
// To send a relative uri you can use the following code:
//
//	strLocation = []byte("Location") // Put this with your top level var () declarations.
//	ctx.Response.Header.SetCanonical(strLocation, "/relative?uri")
//	ctx.Response.SetStatusCode(fasthttp.StatusMovedPermanently)
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
