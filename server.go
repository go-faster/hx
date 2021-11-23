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

	"github.com/go-faster/errors"
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

// Nop is handler that does nothing.
func Nop(_ *Ctx) {}

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
	noCopy noCopy // nolint:unused,structcheck

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

	// Whether to enable tcp keep-alive connections.
	//
	// Whether the operating system should send tcp keep-alive messages on the tcp connection.
	//
	// By default tcp keep-alive connections are disabled.
	TCPKeepalive bool

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
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	if s.Handler == nil {
		s.Handler = Nop
	}
	ln, err := net.Listen("tcp4", addr)
	if err != nil {
		return err
	}
	if tcpLn, ok := ln.(*net.TCPListener); ok {
		return s.Serve(ctx, tcpKeepaliveListener{
			TCPListener:     tcpLn,
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
	if s.Handler == nil {
		s.Handler = Nop
	}

	workers := s.getWorkers()

	s.mu.Lock()
	{
		s.ln = append(s.ln, ln)
		if s.done == nil {
			s.done = make(chan struct{})
		}
	}
	s.mu.Unlock()

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-gCtx.Done()
		close(s.done)
		return nil
	})

	for i := 0; i < workers; i++ {
		g.Go(func() error {
			var (
				c   net.Conn
				err error
			)

			reqCtx := &Ctx{}
			r := bufio.NewReader(nil)
			w := bufio.NewWriter(nil)

			for {
				if gCtx.Err() != nil {
					return gCtx.Err()
				}
				if c, err = s.accept(ln); err != nil {
					if err == io.EOF {
						return nil
					}
					return err
				}

				reqCtx.ResetBody()
				reqCtx.c = c
				r.Reset(c)
				w.Reset(c)

				s.setState(c, StateNew)
				atomic.AddInt32(&s.open, 1)
				err = s.serveConn(reqCtx, r, w)
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
	if s.Handler == nil {
		s.Handler = Nop
	}

	atomic.AddInt32(&s.open, 1)

	err := s.serveConn(&Ctx{c: c}, bufio.NewReader(c), bufio.NewWriter(c))

	closeErr := c.Close()
	s.setState(c, StateClosed)
	if err == nil {
		err = closeErr
	}

	atomic.AddInt32(&s.open, -1)

	return err
}

// GetOpenConnectionsCount returns a number of opened connections.
//
// This function is intended be used by monitoring systems
func (s *Server) GetOpenConnectionsCount() int32 {
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

func (s *Server) serveConn(ctx *Ctx, r *bufio.Reader, w *bufio.Writer) (err error) {
	var serverName []byte
	if !s.NoDefaultServerHeader {
		serverName = s.getServerName()
	}

	requests := uint64(0)
	connID := nextConnID()
	connTime := time.Now()
	writeTimeout := s.WriteTimeout
	previousWriteTimeout := time.Duration(0)
	ctx.connTime = connTime
	c := ctx.c

	var (
		connClose bool
		http11    bool
	)
	for {
		requests++

		// If this is a keep-alive connection set the idle timeout.
		if requests > 1 {
			if d := s.idleTimeout(); d > 0 {
				if err := c.SetReadDeadline(time.Now().Add(d)); err != nil {
					panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", d, err))
				}
			}
		}

		// If this is a keep-alive connection we want to try and read the first bytes
		// within the idle time.
		if requests > 1 {
			var b []byte
			b, err = r.Peek(1)
			if len(b) == 0 {
				// If reading from a keep-alive connection returns nothing it means
				// the connection was closed (either timeout or from the other side).
				if err != io.EOF {
					err = ErrNothingRead{err}
				}
			}
		}

		ctx.Response.Header.noDefaultContentType = s.NoDefaultContentType
		ctx.Response.Header.noDefaultDate = s.NoDefaultDate

		if err == nil {
			if s.ReadTimeout > 0 {
				if err := c.SetReadDeadline(time.Now().Add(s.ReadTimeout)); err != nil {
					panic(fmt.Sprintf("BUG: error in SetReadDeadline(%s): %s", s.ReadTimeout, err))
				}
			} else if s.IdleTimeout > 0 && requests > 1 {
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
			if w.Buffered() > 0 {
				err = ctx.Request.Header.readLoop(r, false)
				if err == errNeedMore {
					err = w.Flush()
					if err != nil {
						break
					}

					err = ctx.Request.Header.Read(r)
				}
			} else {
				err = ctx.Request.Header.Read(r)
			}
			if err == nil {
				err = ctx.Request.readBody(r)
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
				if requests > 1 {
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
				s.writeErrorResponse(w, ctx, serverName, err)
			}

			return err
		}

		// 'Expect: 100-continue' request handling.
		// See https://www.w3.org/Protocols/rfc2616/rfc2616-sec8.html#sec8.2.3 for details.
		if ctx.Request.MayContinue() {
			// Send 'HTTP/1.1 100 Continue' response.
			_, err = w.Write(strResponseContinue)
			if err != nil {
				return errors.Wrap(err, "write")
			}
			if err = w.Flush(); err != nil {
				return errors.Wrap(err, "flush")
			}
			if err := ctx.Request.ContinueReadBody(r); err != nil {
				s.writeErrorResponse(w, ctx, serverName, err)
				return errors.Wrap(err, "body")
			}
		}

		connClose = ctx.Request.Header.ConnectionClose()
		http11 = ctx.Request.Header.IsHTTP11()

		if serverName != nil {
			ctx.Response.Header.SetServerBytes(serverName)
		}
		ctx.connID = connID
		ctx.connRequestNum = requests
		ctx.time = time.Now()
		s.Handler(ctx)
		if ctx.IsHead() {
			ctx.Response.SkipBody = true
		}
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

		connClose = connClose || ctx.Response.ConnectionClose() || (s.CloseOnShutdown && atomic.LoadInt32(&s.stop) == 1)
		if connClose {
			ctx.Response.Header.SetCanonical(strConnection, strClose)
		} else if !http11 {
			// Set 'Connection: keep-alive' response header for non-HTTP/1.1 request.
			// There is no need in setting this header for http/1.1, since in http/1.1
			// connections are keep-alive by default.
			ctx.Response.Header.SetCanonical(strConnection, strKeepAlive)
		}

		if serverName != nil && len(ctx.Response.Header.Server()) == 0 {
			ctx.Response.Header.SetServerBytes(serverName)
		}
		if err := writeResponse(ctx, w); err != nil {
			return err
		}

		// Only flush the writer if we don't have another request in the pipeline.
		// This is a big of an ugly optimization for https://www.techempower.com/benchmarks/
		// This benchmark will send 16 pipelined requests. It is faster to pack as many responses
		// in a TCP packet and send it back at once than waiting for a flush every request.
		// In real world circumstances this behavior could be argued as being wrong.
		if r.Buffered() == 0 || connClose {
			if err := w.Flush(); err != nil {
				return errors.Wrap(err, "flush")
			}
		}
		if connClose {
			return nil
		}

		s.setState(c, StateIdle)

		if atomic.LoadInt32(&s.stop) == 1 {
			return nil
		}
	}

	return nil
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
	_, _ = w.Write(formatStatusLine(nil, strHTTP11, statusCode, s2b(StatusMessage(statusCode))))

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

func (s *Server) writeErrorResponse(bw *bufio.Writer, ctx *Ctx, serverName []byte, err error) {
	errorHandler := defaultErrorHandler
	if s.ErrorHandler != nil {
		errorHandler = s.ErrorHandler
	}

	errorHandler(ctx, err)

	if serverName != nil {
		ctx.Response.Header.SetServerBytes(serverName)
	}
	ctx.SetConnectionClose()
	_ = writeResponse(ctx, bw) //nolint:errcheck
	_ = bw.Flush()
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
