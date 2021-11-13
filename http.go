package hx

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/go-faster/errors"
)

// Request represents HTTP request.
//
// It is forbidden copying Request instances. Create new instances
// and use CopyTo instead.
//
// Request instance MUST NOT be used from concurrently running goroutines.
type Request struct {
	noCopy noCopy //nolint:unused,structcheck

	// Request header
	//
	// Copying Header by value is forbidden. Use pointer to Header instead.
	Header RequestHeader

	uri      URI
	postArgs Args

	body    *Buffer
	bodyRaw []byte

	// Group bool members in order to reduce Request object size.
	parsedURI      bool
	parsedPostArgs bool

	keepBodyBuffer bool

	// Request timeout. Usually set by DoDeadline or DoTimeout
	// if <= 0, means not set
	timeout time.Duration
}

// Response represents HTTP response.
//
// It is forbidden copying Response instances. Create new instances
// and use CopyTo instead.
//
// Response instance MUST NOT be used from concurrently running goroutines.
type Response struct {
	noCopy noCopy //nolint:unused,structcheck

	// Response header
	//
	// Copying Header by value is forbidden. Use pointer to Header instead.
	Header ResponseHeader

	body    *Buffer
	bodyRaw []byte

	// Response.Read() skips reading body if set to true.
	// Use it for reading HEAD responses.
	//
	// Response.Write() skips writing body if set to true.
	// Use it for writing HEAD responses.
	SkipBody bool

	keepBodyBuffer bool

	// Remote TCPAddr from concurrently net.Conn
	raddr net.Addr
	// Local TCPAddr from concurrently net.Conn
	laddr net.Addr
}

// SetHost sets host for the request.
func (r *Request) SetHost(host string) {
	r.URI().SetHost(host)
}

// SetHostBytes sets host for the request.
func (r *Request) SetHostBytes(host []byte) {
	r.URI().SetHostBytes(host)
}

// Host returns the host for the given request.
func (r *Request) Host() []byte {
	return r.URI().Host()
}

// SetRequestURI sets RequestURI.
func (r *Request) SetRequestURI(requestURI string) {
	r.Header.SetRequestURI(requestURI)
	r.parsedURI = false
}

// SetRequestURIBytes sets RequestURI.
func (r *Request) SetRequestURIBytes(requestURI []byte) {
	r.Header.SetRequestURIBytes(requestURI)
	r.parsedURI = false
}

// RequestURI returns request's URI.
func (r *Request) RequestURI() []byte {
	if r.parsedURI {
		requestURI := r.uri.RequestURI()
		r.SetRequestURIBytes(requestURI)
	}
	return r.Header.RequestURI()
}

// StatusCode returns response status code.
func (r *Response) StatusCode() int {
	return r.Header.StatusCode()
}

// SetStatusCode sets response status code.
func (r *Response) SetStatusCode(statusCode int) {
	r.Header.SetStatusCode(statusCode)
}

// ConnectionClose returns true if 'Connection: close' header is set.
func (r *Response) ConnectionClose() bool {
	return r.Header.ConnectionClose()
}

// SetConnectionClose sets 'Connection: close' header.
func (r *Response) SetConnectionClose() {
	r.Header.SetConnectionClose()
}

// ConnectionClose returns true if 'Connection: close' header is set.
func (r *Request) ConnectionClose() bool {
	return r.Header.ConnectionClose()
}

// SetConnectionClose sets 'Connection: close' header.
func (r *Request) SetConnectionClose() {
	r.Header.SetConnectionClose()
}

func (r *Response) parseNetConn(conn net.Conn) {
	r.raddr = conn.RemoteAddr()
	r.laddr = conn.LocalAddr()
}

// RemoteAddr returns the remote network address. The Addr returned is shared
// by all invocations of RemoteAddr, so do not modify it.
func (r *Response) RemoteAddr() net.Addr {
	return r.raddr
}

// LocalAddr returns the local network address. The Addr returned is shared
// by all invocations of LocalAddr, so do not modify it.
func (r *Response) LocalAddr() net.Addr {
	return r.laddr
}

// Body returns response body.
//
// The returned value is valid until the response is released,
// either though ReleaseResponse or your request handler returning.
// Do not store references to returned value. Make copies instead.
func (r *Response) Body() []byte {
	return r.bodyBytes()
}

func (r *Response) bodyBytes() []byte {
	if r.bodyRaw != nil {
		return r.bodyRaw
	}
	if r.body == nil {
		return nil
	}
	return r.body.Buf
}

func (r *Request) bodyBytes() []byte {
	if r.bodyRaw != nil {
		return r.bodyRaw
	}
	if r.body == nil {
		return nil
	}
	return r.body.Buf
}

func (r *Response) bodyBuffer() *Buffer {
	if r.body == nil {
		r.body = responseBodyPool.Get()
	}
	r.bodyRaw = nil
	return r.body
}

func (r *Request) bodyBuffer() *Buffer {
	if r.body == nil {
		r.body = requestBodyPool.Get()
	}
	r.bodyRaw = nil
	return r.body
}

var (
	responseBodyPool = newPool()
	requestBodyPool  = newPool()
)

// BodyWriteTo writes request body to w.
func (r *Request) BodyWriteTo(w io.Writer) error {
	_, err := w.Write(r.bodyBytes())
	return err
}

// BodyWriteTo writes response body to w.
func (r *Response) BodyWriteTo(w io.Writer) error {
	_, err := w.Write(r.bodyBytes())
	return err
}

// AppendBody appends p to response body.
//
// It is safe re-using p after the function returns.
func (r *Response) AppendBody(p []byte) {
	r.bodyBuffer().Put(p) //nolint:errcheck
}

// AppendBodyString appends s to response body.
func (r *Response) AppendBodyString(s string) {
	r.bodyBuffer().PutString(s) //nolint:errcheck
}

// SetBody sets response body.
//
// It is safe re-using body argument after the function returns.
func (r *Response) SetBody(body []byte) {
	bodyBuf := r.bodyBuffer()
	bodyBuf.Reset()
	bodyBuf.Put(body)
}

// SetBodyString sets response body.
func (r *Response) SetBodyString(body string) {
	bodyBuf := r.bodyBuffer()
	bodyBuf.Reset()
	bodyBuf.PutString(body)
}

// ResetBody resets response body.
func (r *Response) ResetBody() {
	r.bodyRaw = nil
	if r.body != nil {
		if r.keepBodyBuffer {
			r.body.Reset()
		} else {
			responseBodyPool.Put(r.body)
			r.body = nil
		}
	}
}

// SetBodyRaw sets response body, but without copying it.
//
// From this point onward the body argument must not be changed.
func (r *Response) SetBodyRaw(body []byte) {
	r.ResetBody()
	r.bodyRaw = body
}

// SetBodyRaw sets response body, but without copying it.
//
// From this point onward the body argument must not be changed.
func (r *Request) SetBodyRaw(body []byte) {
	r.ResetBody()
	r.bodyRaw = body
}

// ReleaseBody retires the response body if it is greater than "size" bytes.
//
// This permits GC to reclaim the large buffer.  If used, must be before
// ReleaseResponse.
//
// Use this method only if you really understand how it works.
// The majority of workloads don't need this method.
func (r *Response) ReleaseBody(size int) {
	r.bodyRaw = nil
	if cap(r.body.Buf) > size {
		r.body = nil
	}
}

// ReleaseBody retires the request body if it is greater than "size" bytes.
//
// This permits GC to reclaim the large buffer.  If used, must be before
// ReleaseRequest.
//
// Use this method only if you really understand how it works.
// The majority of workloads don't need this method.
func (r *Request) ReleaseBody(size int) {
	r.bodyRaw = nil
	if cap(r.body.Buf) > size {
		r.body = nil
	}
}

// Body returns request body.
//
// The returned value is valid until the request is released,
// either though ReleaseRequest or your request handler returning.
// Do not store references to returned value. Make copies instead.
func (r *Request) Body() []byte {
	if r.bodyRaw != nil {
		return r.bodyRaw
	}
	return r.bodyBytes()
}

// AppendBody appends p to request body.
//
// It is safe re-using p after the function returns.
func (r *Request) AppendBody(p []byte) {
	r.bodyBuffer().Put(p)
}

// AppendBodyString appends s to request body.
func (r *Request) AppendBodyString(s string) {
	r.bodyBuffer().PutString(s)
}

// SetBody sets request body.
//
// It is safe re-using body argument after the function returns.
func (r *Request) SetBody(body []byte) {
	r.bodyBuffer().Reset()
	r.bodyBuffer().Put(body)
}

// SetBodyString sets request body.
func (r *Request) SetBodyString(body string) {
	r.bodyBuffer().Reset()
	r.bodyBuffer().PutString(body)
}

// ResetBody resets request body.
func (r *Request) ResetBody() {
	r.bodyRaw = nil
	if r.body != nil {
		if r.keepBodyBuffer {
			r.body.Reset()
		} else {
			requestBodyPool.Put(r.body)
			r.body = nil
		}
	}
}

// CopyTo copies req contents to dst except of body stream.
func (r *Request) CopyTo(dst *Request) {
	r.copyToSkipBody(dst)
	if r.bodyRaw != nil {
		dst.bodyRaw = r.bodyRaw
		if dst.body != nil {
			dst.body.Reset()
		}
	} else if r.body != nil {
		dst.SetBody(r.bodyBytes())
	} else if dst.body != nil {
		dst.body.Reset()
	}
}

func (r *Request) copyToSkipBody(dst *Request) {
	dst.Reset()
	r.Header.CopyTo(&dst.Header)

	r.uri.CopyTo(&dst.uri)
	dst.parsedURI = r.parsedURI

	r.postArgs.CopyTo(&dst.postArgs)
	dst.parsedPostArgs = r.parsedPostArgs
}

// CopyTo copies resp contents to dst except of body stream.
func (r *Response) CopyTo(dst *Response) {
	r.copyToSkipBody(dst)
	if r.bodyRaw != nil {
		dst.bodyRaw = r.bodyRaw
		if dst.body != nil {
			dst.body.Reset()
		}
	} else if r.body != nil {
		dst.SetBody(r.bodyBytes())
	} else if dst.body != nil {
		dst.body.Reset()
	}
}

func (r *Response) copyToSkipBody(dst *Response) {
	dst.Reset()
	r.Header.CopyTo(&dst.Header)
	dst.SkipBody = r.SkipBody
	dst.raddr = r.raddr
	dst.laddr = r.laddr
}

// URI returns request URI
func (r *Request) URI() *URI {
	_ = r.parseURI()
	return &r.uri
}

// SetURI initializes request URI
// Use this method if a single URI may be reused across multiple requests.
// Otherwise, you can just use SetRequestURI() and it will be parsed as new URI.
// The URI is copied and can be safely modified later.
func (r *Request) SetURI(uri *URI) {
	if uri != nil {
		uri.CopyTo(&r.uri)
		r.parsedURI = true
		return
	}
	r.uri.Reset()
	r.parsedURI = false
}

func (r *Request) parseURI() error {
	if r.parsedURI {
		return nil
	}
	r.parsedURI = true

	return r.uri.parse(r.Header.Host(), r.Header.RequestURI())
}

// PostArgs returns POST arguments.
func (r *Request) PostArgs() *Args {
	r.parsePostArgs()
	return &r.postArgs
}

func (r *Request) parsePostArgs() {
	if r.parsedPostArgs {
		return
	}
	r.parsedPostArgs = true

	if !bytes.HasPrefix(r.Header.ContentType(), strPostArgsContentType) {
		return
	}
	r.postArgs.ParseBytes(r.bodyBytes())
}

// Reset clears request contents.
func (r *Request) Reset() {
	r.Header.Reset()
	r.resetSkipHeader()
	r.timeout = 0
}

func (r *Request) resetSkipHeader() {
	r.ResetBody()
	r.uri.Reset()
	r.parsedURI = false
	r.postArgs.Reset()
	r.parsedPostArgs = false
}

// Reset clears response contents.
func (r *Response) Reset() {
	r.Header.Reset()
	r.resetSkipHeader()
	r.SkipBody = false
	r.raddr = nil
	r.laddr = nil
}

func (r *Response) resetSkipHeader() {
	r.ResetBody()
}

// Read reads request (including body) from the given r.
//
// RemoveMultipartFormFiles or Reset must be called after
// reading multipart/form-data request in order to delete temporarily
// uploaded files.
//
// If MayContinue returns true, the caller must:
//
//     - Either send StatusExpectationFailed response if request headers don't
//       satisfy the caller.
//     - Or send StatusContinue response before reading request body
//       with ContinueReadBody.
//     - Or close the connection.
//
// io.EOF is returned if r is closed before reading the first header byte.
func (r *Request) Read(reader *bufio.Reader) error {
	r.resetSkipHeader()
	if err := r.Header.Read(reader); err != nil {
		return errors.Wrap(err, "header")
	}

	return r.readBody(reader)
}

func (r *Request) readBody(reader *bufio.Reader) error {
	// Do not reset the request here - the caller must reset it before
	// calling this method.
	if r.MayContinue() {
		// 'Expect: 100-continue' header found. Let the caller deciding
		// whether to read request body or
		// to return StatusExpectationFailed.
		return nil
	}

	return r.ContinueReadBody(reader)
}

// MayContinue returns true if the request contains
// 'Expect: 100-continue' header.
//
// The caller must do one of the following actions if MayContinue returns true:
//
//     - Either send StatusExpectationFailed response if request headers don't
//       satisfy the caller.
//     - Or send StatusContinue response before reading request body
//       with ContinueReadBody.
//     - Or close the connection.
func (r *Request) MayContinue() bool {
	return bytes.Equal(r.Header.peek(strExpect), str100Continue)
}

const (
	lenIdentity = -2 // identity body
	lenChunked  = -1 // chunk encoded
)

// ContinueReadBody reads request body if request header contains
// 'Expect: 100-continue'.
//
// The caller must send StatusContinue response before calling this method.
//
// If maxBodySize > 0 and the body size exceeds maxBodySize,
// then ErrBodyTooLarge is returned.
func (r *Request) ContinueReadBody(reader *bufio.Reader) error {
	l := r.Header.realContentLength()
	if l == lenIdentity {
		// identity body has no sense for http requests, since
		// the end of body is determined by connection close.
		// So just ignore request body for requests without
		// 'Content-Length' and 'Transfer-Encoding' headers.
		// refer to https://tools.ietf.org/html/rfc7230#section-3.3.2
		if !r.Header.ignoreBody() {
			r.Header.SetContentLength(0)
		}
		return nil
	}

	return r.ReadBody(reader, l)
}

// ReadBody reads request body from the given r, limiting the body size.
//
// If maxBodySize > 0 and the body size exceeds maxBodySize,
// then ErrBodyTooLarge is returned.
func (r *Request) ReadBody(reader *bufio.Reader, contentLength int) error {
	b := r.bodyBuffer()
	b.Reset()
	if err := readBody(reader, contentLength, b); err != nil {
		r.Reset()
		return errors.Wrap(err, "read")
	}
	r.Header.SetContentLength(b.Len())
	return nil
}

// Read reads response (including body) from the given r.
//
// io.EOF is returned if r is closed before reading the first header byte.
func (r *Response) Read(reader *bufio.Reader) error {
	r.resetSkipHeader()
	if err := r.Header.Read(reader); err != nil {
		return err
	}
	if r.Header.StatusCode() == StatusContinue {
		// Read the next response according to http://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html .
		if err := r.Header.Read(reader); err != nil {
			return err
		}
	}
	if !r.mustSkipBody() {
		return r.readBody(reader)
	}

	return nil
}

// readBody reads response body from the given r, limiting the body size.
//
// If maxBodySize > 0 and the body size exceeds maxBodySize,
// then ErrBodyTooLarge is returned.
func (r *Response) readBody(reader *bufio.Reader) error {
	b := r.bodyBuffer()
	b.Reset()
	if err := readBody(reader, r.Header.ContentLength(), b); err != nil {
		return err
	}
	r.Header.SetContentLength(b.Len())
	return nil
}

func (r *Response) mustSkipBody() bool {
	return r.SkipBody || r.Header.mustSkipContentLength()
}

var errRequestHostRequired = errors.New("missing required Host header in request")

// WriteTo writes request to w. It implements io.WriterTo.
func (r *Request) WriteTo(w io.Writer) (int64, error) {
	return writeBufio(r, w)
}

// WriteTo writes response to w. It implements io.WriterTo.
func (r *Response) WriteTo(w io.Writer) (int64, error) {
	return writeBufio(r, w)
}

func writeBufio(hw httpWriter, w io.Writer) (int64, error) {
	sw := acquireStatsWriter(w)
	bw := acquireBufioWriter(sw)
	err1 := hw.Write(bw)
	err2 := bw.Flush()
	releaseBufioWriter(bw)
	n := sw.bytesWritten
	releaseStatsWriter(sw)

	err := err1
	if err == nil {
		err = err2
	}
	return n, err
}

type statsWriter struct {
	w            io.Writer
	bytesWritten int64
}

func (w *statsWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.bytesWritten += int64(n)
	return n, err
}

func acquireStatsWriter(w io.Writer) *statsWriter {
	v := statsWriterPool.Get()
	if v == nil {
		return &statsWriter{
			w: w,
		}
	}
	sw := v.(*statsWriter)
	sw.w = w
	return sw
}

func releaseStatsWriter(sw *statsWriter) {
	sw.w = nil
	sw.bytesWritten = 0
	statsWriterPool.Put(sw)
}

var statsWriterPool sync.Pool

func acquireBufioWriter(w io.Writer) *bufio.Writer {
	v := bufioWriterPool.Get()
	if v == nil {
		return bufio.NewWriter(w)
	}
	bw := v.(*bufio.Writer)
	bw.Reset(w)
	return bw
}

func releaseBufioWriter(bw *bufio.Writer) {
	bufioWriterPool.Put(bw)
}

var bufioWriterPool sync.Pool

// Write writes request to w.
//
// Write doesn't flush request to w for performance reasons.
//
// See also WriteTo.
func (r *Request) Write(w *bufio.Writer) error {
	if len(r.Header.Host()) == 0 || r.parsedURI {
		uri := r.URI()
		host := uri.Host()
		if len(host) == 0 {
			return errRequestHostRequired
		}
		r.Header.SetHostBytes(host)
		r.Header.SetRequestURIBytes(uri.RequestURI())

		if len(uri.username) > 0 {
			// RequestHeader.SetBytesKV only uses RequestHeader.bufKV.key
			// So we are free to use RequestHeader.bufKV.value as a scratch pad for
			// the base64 encoding.
			nl := len(uri.username) + len(uri.password) + 1
			nb := nl + len(strBasicSpace)
			tl := nb + base64.StdEncoding.EncodedLen(nl)
			if tl > cap(r.Header.bufKV.value) {
				r.Header.bufKV.value = make([]byte, 0, tl)
			}
			buf := r.Header.bufKV.value[:0]
			buf = append(buf, uri.username...)
			buf = append(buf, strColon...)
			buf = append(buf, uri.password...)
			buf = append(buf, strBasicSpace...)
			base64.StdEncoding.Encode(buf[nb:tl], buf[:nl])
			r.Header.SetBytesKV(strAuthorization, buf[nl:tl])
		}
	}

	body := r.bodyBytes()
	var err error

	hasBody := false
	if len(body) == 0 {
		body = r.postArgs.QueryString()
	}
	if len(body) != 0 || !r.Header.ignoreBody() {
		hasBody = true
		r.Header.SetContentLength(len(body))
	}
	if err = r.Header.Write(w); err != nil {
		return err
	}
	if hasBody {
		_, err = w.Write(body)
	} else if len(body) > 0 {
		return fmt.Errorf("non-zero body for non-POST request. body=%q", body)
	}
	return err
}

// Write writes response to w.
//
// Write doesn't flush response to w for performance reasons.
//
// See also WriteTo.
func (r *Response) Write(w *bufio.Writer) error {
	sendBody := !r.mustSkipBody()

	body := r.bodyBytes()
	bodyLen := len(body)
	if sendBody || bodyLen > 0 {
		r.Header.SetContentLength(bodyLen)
	}
	if err := r.Header.Write(w); err != nil {
		return err
	}
	if sendBody {
		if _, err := w.Write(body); err != nil {
			return err
		}
	}
	return nil
}

// String returns request representation.
//
// Returns error message instead of request representation on error.
//
// Use Write instead of String for performance-critical code.
func (r *Request) String() string {
	return getHTTPString(r)
}

// String returns response representation.
//
// Returns error message instead of response representation on error.
//
// Use Write instead of String for performance-critical code.
func (r *Response) String() string {
	return getHTTPString(r)
}

func getHTTPString(hw httpWriter) string {
	b := bufPool.Get()
	defer bufPool.Put(b)
	w := bufio.NewWriter(b)
	if err := hw.Write(w); err != nil {
		return err.Error()
	}
	if err := w.Flush(); err != nil {
		return err.Error()
	}
	return b.String()
}

type httpWriter interface {
	Write(w *bufio.Writer) error
}

// ErrBodyTooLarge is returned if either request or response body exceeds
// the given limit.
var ErrBodyTooLarge = errors.New("body size exceeds the given limit")

func readBody(r *bufio.Reader, contentLength int, buf *Buffer) error {
	switch {
	case contentLength >= 0:
		if err := readBodyFixed(r, buf, contentLength); err != nil {
			return errors.Wrap(err, "fixed")
		}
	case contentLength == lenChunked:
		if err := readBodyChunked(r, buf); err != nil {
			return errors.Wrap(err, "chunked")
		}
	default:
		if err := readBodyIdentity(r, buf); err != nil {
			return errors.Wrap(err, "identity")
		}
	}

	return nil
}

func readBodyIdentity(r *bufio.Reader, b *Buffer) error {
	_, err := r.WriteTo(b)
	return err
}

func readBodyFixed(r *bufio.Reader, b *Buffer, n int) error {
	if n == 0 {
		return nil
	}

	start := b.Len()
	b.Expand(n)
	if _, err := io.ReadFull(r, b.Buf[start:start+n]); err != nil {
		return errors.Wrapf(err, "read full %d", n)
	}

	return nil
}

// ErrBrokenChunk is returned when server receives a broken chunked body (Transfer-Encoding: chunked).
type ErrBrokenChunk struct {
	Reason string
}

func (e *ErrBrokenChunk) Error() string {
	return fmt.Sprintf("broken chunk: %s", e.Reason)
}

func readBodyChunked(r *bufio.Reader, b *Buffer) error {
	const crlfLen = 2
	for {
		start := len(b.Buf)
		chunkSize, err := parseChunkSize(r)
		if err != nil {
			return errors.Wrap(err, "size")
		}
		if err := readBodyFixed(r, b, chunkSize+crlfLen); err != nil {
			return errors.Wrap(err, "fixed")
		}
		if b.Len() < crlfLen {
			return io.ErrUnexpectedEOF
		}
		if !bytes.Equal(strCRLF, b.Buf[start+chunkSize:]) {
			return errors.Wrap(&ErrBrokenChunk{Reason: "no crlf at the end"}, "not equal")
		}
		b.Buf = b.Buf[:start+chunkSize]
		if chunkSize == 0 {
			return nil
		}
	}
}

func parseChunkSize(r *bufio.Reader) (int, error) {
	n, err := readHexInt(r)
	if err != nil {
		return -1, errors.Wrap(err, "read hex int")
	}
	for {
		c, err := r.ReadByte()
		if err != nil {
			return -1, &ErrBrokenChunk{
				Reason: fmt.Sprintf("cannot read '\r' char at the end of chunk size: %s", err),
			}
		}
		// Skip any trailing whitespace after chunk size.
		if c == ' ' {
			continue
		}
		if err := r.UnreadByte(); err != nil {
			return -1, &ErrBrokenChunk{
				Reason: fmt.Sprintf("cannot unread '\r' char at the end of chunk size: %s", err),
			}
		}
		break
	}
	if err := readCRLF(r); err != nil {
		return -1, errors.Wrap(err, "read crlf")
	}
	return n, nil
}

func readCRLF(r *bufio.Reader) error {
	for _, exp := range []byte{'\r', '\n'} {
		c, err := r.ReadByte()
		if err != nil {
			return &ErrBrokenChunk{
				Reason: fmt.Sprintf("cannot read %q char at the end of chunk size: %s", exp, err),
			}
		}
		if c != exp {
			return &ErrBrokenChunk{
				Reason: fmt.Sprintf("unexpected char %q at the end of chunk size. Expected %q", c, exp),
			}
		}
	}
	return nil
}

func round2(n int) int {
	if n <= 0 {
		return 0
	}

	x := uint32(n - 1)
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16

	return int(x + 1)
}
