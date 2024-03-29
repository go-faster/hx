// go:build !windows || !race

package hx

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-faster/hx/hxutil"
)

// Make sure Ctx implements context.Context
var _ context.Context = &Ctx{}

func TestServerCRNLAfterPost_Pipeline(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
		},
	}

	ln := hxutil.NewInmemoryListener()
	defer ln.Close()

	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	}()

	c, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("POST / HTTP/1.1\r\nHost: golang.org\r\nContent-Length: 3\r\n\r\nABC" +
		"\r\n\r\n" + // <-- this stuff is bogus, but we'll ignore it
		"GET / HTTP/1.1\r\nHost: golang.org\r\n\r\n")); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)
	var resp Response
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
	}
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
	}
}

func TestServerCRNLAfterPost(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
		},
		ReadTimeout: time.Millisecond * 100,
	}

	ln := hxutil.NewInmemoryListener()
	defer ln.Close()

	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	}()

	c, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer c.Close()
	if _, err = c.Write([]byte("POST / HTTP/1.1\r\nHost: golang.org\r\nContent-Length: 3\r\n\r\nABC" +
		"\r\n\r\n", // <-- this stuff is bogus, but we'll ignore it
	)); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)
	var resp Response
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
	}
	if err := resp.Read(br); err == nil {
		t.Fatal("expected error") // We didn't send a request so we should get an error here.
	}
}

func TestServerPipelineFlush(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
		},
	}
	ln := hxutil.NewInmemoryListener()

	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	}()

	c, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if _, err = c.Write([]byte("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")); err != nil {
		t.Fatal(err)
	}

	// Write a partial request.
	if _, err = c.Write([]byte("GET /foo1 HTTP/1.1\r\nHost: ")); err != nil {
		t.Fatal(err)
	}
	go func() {
		// Wait for 200ms to finish the request
		time.Sleep(time.Millisecond * 200)

		if _, err = c.Write([]byte("google.com\r\n\r\n")); err != nil {
			t.Error(err)
		}
	}()

	start := time.Now()
	br := bufio.NewReader(c)
	var resp Response

	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
	}

	// Since the second request takes 200ms to finish we expect the first one to be flushed earlier.
	d := time.Since(start)
	if d > time.Millisecond*100 {
		t.Fatalf("had to wait for %v", d)
	}

	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
	}
}

func TestServerInvalidHeader(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			if ctx.Request.Header.Peek("Foo") != nil || ctx.Request.Header.Peek("Foo ") != nil {
				t.Error("expected Foo header")
			}
		},
	}

	ln := hxutil.NewInmemoryListener()

	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	}()

	c, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if _, err = c.Write([]byte("POST /foo HTTP/1.1\r\nHost: gle.com\r\nFoo : bar\r\nContent-Length: 5\r\n\r\n12345")); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)
	var resp Response
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusBadRequest {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusBadRequest)
	}

	c, err = ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if _, err = c.Write([]byte("GET /foo HTTP/1.1\r\nHost: gle.com\r\nFoo : bar\r\n\r\n")); err != nil {
		t.Fatal(err)
	}

	br = bufio.NewReader(c)
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if resp.StatusCode() != StatusBadRequest {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusBadRequest)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestServerConnState(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	states := make([]string, 0)
	s := &Server{
		Handler: func(ctx *Ctx) {},
		ConnState: func(conn net.Conn, state ConnState) {
			states = append(states, state.String())
		},
	}

	ln := hxutil.NewInmemoryListener()

	serverCh := make(chan struct{})
	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		close(serverCh)
	}()

	clientCh := make(chan struct{})
	go func() {
		c, err := ln.Dial()
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		br := bufio.NewReader(c)
		// Send 2 requests on the same connection.
		for i := 0; i < 2; i++ {
			if _, err = c.Write([]byte("GET / HTTP/1.1\r\nHost: aa\r\n\r\n")); err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			var resp Response
			if err := resp.Read(br); err != nil {
				t.Errorf("unexpected error: %s", err)
			}
			if resp.StatusCode() != StatusOK {
				t.Errorf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusOK)
			}
		}
		if err := c.Close(); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		// Give the server a little bit of time to transition the connection to the close state.
		time.Sleep(time.Millisecond * 100)
		close(clientCh)
	}()

	select {
	case <-clientCh:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	select {
	case <-serverCh:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	// 2 requests so we go to active and idle twice.
	expected := []string{"new", "active", "idle", "active", "idle", "closed"}

	if !reflect.DeepEqual(expected, states) {
		t.Fatalf("wrong state, expected %s, got %s", expected, states)
	}
}

func TestServerName(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {},
	}

	getResponse := func() []byte {
		rw := &readWriter{}
		rw.r.WriteString("GET / HTTP/1.1\r\nHost: google.com\r\n\r\n")

		if err := s.ServeConn(rw); err != nil {
			t.Fatalf("Unexpected error from serveConn: %s", err)
		}

		resp, err := io.ReadAll(&rw.w)
		if err != nil {
			t.Fatalf("Unexpected error from ReadAll: %s", err)
		}

		return resp
	}

	resp := getResponse()
	if !bytes.Contains(resp, []byte("\r\nServer: "+string(defaultServerName)+"\r\n")) {
		t.Fatalf("Unexpected response %q expected Server: "+string(defaultServerName), resp)
	}

	// We can't just overwrite s.Name as fasthttp caches the name in an atomic.Value
	s = &Server{
		Handler: func(ctx *Ctx) {
		},
		Name: "foobar",
	}

	resp = getResponse()
	if !bytes.Contains(resp, []byte("\r\nServer: foobar\r\n")) {
		t.Fatalf("Unexpected response %q expected Server: foobar", resp)
	}

	s = &Server{
		Handler: func(ctx *Ctx) {
		},
		NoDefaultServerHeader: true,
		NoDefaultContentType:  true,
		NoDefaultDate:         true,
	}

	resp = getResponse()
	if bytes.Contains(resp, []byte("\r\nServer: ")) {
		t.Fatalf("Unexpected response %q expected no Server header", resp)
	}

	if bytes.Contains(resp, []byte("\r\nContent-Type: ")) {
		t.Fatalf("Unexpected response %q expected no Content-Type header", resp)
	}

	if bytes.Contains(resp, []byte("\r\nDate: ")) {
		t.Fatalf("Unexpected response %q expected no Date header", resp)
	}
}

func TestRequestCtxString(t *testing.T) {
	t.Parallel()

	var ctx Ctx

	s := ctx.String()
	expectedS := "#0000000000000000 - 0.0.0.0:0<->0.0.0.0:0 - GET http:///"
	if s != expectedS {
		t.Fatalf("unexpected ctx.String: %q. Expecting %q", s, expectedS)
	}

	ctx.Request.SetRequestURI("https://foobar.com/aaa?bb=c")
	s = ctx.String()
	expectedS = "#0000000000000000 - 0.0.0.0:0<->0.0.0.0:0 - GET https://foobar.com/aaa?bb=c"
	if s != expectedS {
		t.Fatalf("unexpected ctx.String: %q. Expecting %q", s, expectedS)
	}
}

func TestServerResponseServerHeader(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	serverName := "foobar serv"

	s := &Server{
		Handler: func(ctx *Ctx) {
			name := ctx.Response.Header.Server()
			if string(name) != serverName {
				fmt.Fprintf(ctx, "unexpected server name: %q. Expecting %q", name, serverName)
			} else {
				ctx.WriteString("OK") //nolint:errcheck
			}

			// make sure the server name is sent to the client after ctx.Response.Reset()
			ctx.NotFound()
		},
		Name: serverName,
	}

	ln := hxutil.NewInmemoryListener()

	serverCh := make(chan struct{})
	go func() {
		if err := s.Serve(context.TODO(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		close(serverCh)
	}()

	clientCh := make(chan struct{})
	go func() {
		c, err := ln.Dial()
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		if _, err = c.Write([]byte("GET / HTTP/1.1\r\nHost: aa\r\n\r\n")); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		br := bufio.NewReader(c)
		var resp Response
		if err = resp.Read(br); err != nil {
			t.Errorf("unexpected error: %s", err)
		}

		if resp.StatusCode() != StatusNotFound {
			t.Errorf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusNotFound)
		}
		if string(resp.Body()) != "404 Page not found" {
			t.Errorf("unexpected body: %q. Expecting %q", resp.Body(), "404 Page not found")
		}
		if string(resp.Header.Server()) != serverName {
			t.Errorf("unexpected server header: %q. Expecting %q", resp.Header.Server(), serverName)
		}
		if err = c.Close(); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		close(clientCh)
	}()

	select {
	case <-clientCh:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	select {
	case <-serverCh:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestServerWriteFastError(t *testing.T) {
	t.Parallel()

	s := &Server{
		Name: "foobar",
	}
	var buf bytes.Buffer
	expectedBody := "access denied"
	s.writeFastError(&buf, StatusForbidden, expectedBody)

	br := bufio.NewReader(&buf)
	var resp Response
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if resp.StatusCode() != StatusForbidden {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.StatusCode(), StatusForbidden)
	}
	body := resp.Body()
	if string(body) != expectedBody {
		t.Fatalf("unexpected body: %q. Expecting %q", body, expectedBody)
	}
	server := string(resp.Header.Server())
	if server != s.Name {
		t.Fatalf("unexpected server: %q. Expecting %q", server, s.Name)
	}
	contentType := string(resp.Header.ContentType())
	if contentType != "text/plain" {
		t.Fatalf("unexpected content-type: %q. Expecting %q", contentType, "text/plain")
	}
	if !resp.Header.ConnectionClose() {
		t.Fatal("expecting 'Connection: close' response header")
	}
}

func TestServerGetWithContent(t *testing.T) {
	t.Parallel()

	h := func(ctx *Ctx) {
		ctx.Success("foo/bar", []byte("success"))
	}
	s := &Server{
		Handler: h,
	}

	rw := &readWriter{}
	rw.r.WriteString("GET / HTTP/1.1\r\nHost: mm.com\r\nContent-Length: 5\r\n\r\nabcde")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	resp := rw.w.String()
	if !strings.HasSuffix(resp, "success") {
		t.Fatalf("unexpected response %s.", resp)
	}
}

func TestServerDisableHeaderNamesNormalizing(t *testing.T) {
	t.Parallel()

	headerName := "CASE-senSITive-HEAder-NAME"
	headerNameLower := strings.ToLower(headerName)
	headerValue := "foobar baz"
	s := &Server{
		Handler: func(ctx *Ctx) {
			hv := ctx.Request.Header.Peek(headerName)
			if string(hv) != headerValue {
				t.Errorf("unexpected header value for %q: %q. Expecting %q", headerName, hv, headerValue)
			}
			hv = ctx.Request.Header.Peek(headerNameLower)
			if len(hv) > 0 {
				t.Errorf("unexpected header value for %q: %q. Expecting empty value", headerNameLower, hv)
			}
			ctx.Response.Header.Set(headerName, headerValue)
			ctx.WriteString("ok") //nolint:errcheck
			ctx.SetContentType("aaa")
		},
		DisableHeaderNamesNormalizing: true,
	}

	rw := &readWriter{}
	rw.r.WriteString(fmt.Sprintf("GET / HTTP/1.1\r\n%s: %s\r\nHost: google.com\r\n\r\n", headerName, headerValue))

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	var resp Response
	resp.Header.DisableNormalizing()
	if err := resp.Read(br); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	hv := resp.Header.Peek(headerName)
	if string(hv) != headerValue {
		t.Fatalf("unexpected header value for %q: %q. Expecting %q", headerName, hv, headerValue)
	}
	hv = resp.Header.Peek(headerNameLower)
	if len(hv) > 0 {
		t.Fatalf("unexpected header value for %q: %q. Expecting empty value", headerNameLower, hv)
	}
}
func TestServerHTTP10ConnectionKeepAlive(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	ln := hxutil.NewInmemoryListener()

	ch := make(chan struct{})
	go func() {
		s := &Server{
			Handler: func(ctx *Ctx) {
				if string(ctx.Path()) == "/close" {
					ctx.SetConnectionClose()
				}
			},
		}
		if err := s.Serve(context.Background(), ln); err != nil {
			t.Errorf("unexpected error: %s", err)
		}
		close(ch)
	}()

	conn, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	_, err = fmt.Fprintf(conn, "%s", "GET / HTTP/1.0\r\nHost: aaa\r\nConnection: keep-alive\r\n\r\n")
	if err != nil {
		t.Fatalf("error when writing request: %s", err)
	}
	_, err = fmt.Fprintf(conn, "%s", "GET /close HTTP/1.0\r\nHost: aaa\r\nConnection: keep-alive\r\n\r\n")
	if err != nil {
		t.Fatalf("error when writing request: %s", err)
	}

	br := bufio.NewReader(conn)
	var resp Response
	if err = resp.Read(br); err != nil {
		t.Fatalf("error when reading response: %s", err)
	}
	if resp.ConnectionClose() {
		t.Fatal("response mustn't have 'Connection: close' header")
	}
	if err = resp.Read(br); err != nil {
		t.Fatalf("error when reading response: %s", err)
	}
	if !resp.ConnectionClose() {
		t.Fatal("response must have 'Connection: close' header")
	}

	tailCh := make(chan struct{})
	go func() {
		tail, err := io.ReadAll(br)
		if err != nil {
			t.Errorf("error when reading tail: %s", err)
		}
		if len(tail) > 0 {
			t.Errorf("unexpected non-zero tail %q", tail)
		}
		close(tailCh)
	}()

	select {
	case <-tailCh:
	case <-time.After(time.Second):
		t.Fatal("timeout when reading tail")
	}

	if err = conn.Close(); err != nil {
		t.Fatalf("error when closing the connection: %s", err)
	}

	if err = ln.Close(); err != nil {
		t.Fatalf("error when closing listener: %s", err)
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timeout when waiting for the server to stop")
	}
}

func TestServerHTTP10ConnectionClose(t *testing.T) {
	t.Skip("TODO")
	t.Parallel()

	ln := hxutil.NewInmemoryListener()

	ch := make(chan struct{})
	go func() {
		s := &Server{
			Handler: func(ctx *Ctx) {
				// The server must close the connection irregardless
				// of request and response state set inside request
				// handler, since the HTTP/1.0 request
				// had no 'Connection: keep-alive' header.
				ctx.Request.Header.ResetConnectionClose()
				ctx.Request.Header.Set(HeaderConnection, "keep-alive")
				ctx.Response.Header.ResetConnectionClose()
				ctx.Response.Header.Set(HeaderConnection, "keep-alive")
			},
		}
		assert.NoError(t, s.Serve(context.Background(), ln))
		close(ch)
	}()

	conn, err := ln.Dial()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	_, err = fmt.Fprintf(conn, "%s", "GET / HTTP/1.0\r\nHost: aaa\r\n\r\n")
	if err != nil {
		t.Fatalf("error when writing request: %s", err)
	}

	br := bufio.NewReader(conn)
	var resp Response
	if err = resp.Read(br); err != nil {
		t.Fatalf("error when reading response: %s", err)
	}

	if !resp.ConnectionClose() {
		t.Fatal("HTTP1.0 response must have 'Connection: close' header")
	}

	tailCh := make(chan struct{})
	go func() {
		tail, err := io.ReadAll(br)
		if err != nil {
			t.Errorf("error when reading tail: %s", err)
		}
		if len(tail) > 0 {
			t.Errorf("unexpected non-zero tail %q", tail)
		}
		close(tailCh)
	}()

	select {
	case <-tailCh:
	case <-time.After(time.Second):
		t.Fatal("timeout when reading tail")
	}

	if err = conn.Close(); err != nil {
		t.Fatalf("error when closing the connection: %s", err)
	}

	if err = ln.Close(); err != nil {
		t.Fatalf("error when closing listener: %s", err)
	}

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timeout when waiting for the server to stop")
	}
}

func TestServerHeadRequest(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			fmt.Fprintf(ctx, "Request method is %q", ctx.Method())
			ctx.SetContentType("aaa/bbb")
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("HEAD /foobar HTTP/1.1\r\nHost: aaa.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	var resp Response
	resp.SkipBody = true
	if err := resp.Read(br); err != nil {
		t.Fatalf("Unexpected error when parsing response: %s", err)
	}
	if resp.Header.StatusCode() != StatusOK {
		t.Fatalf("unexpected status code: %d. Expecting %d", resp.Header.StatusCode(), StatusOK)
	}
	if len(resp.Body()) > 0 {
		t.Fatalf("Unexpected non-zero body %q", resp.Body())
	}
	if resp.Header.ContentLength() != 24 {
		t.Fatalf("unexpected content-length %d. Expecting %d", resp.Header.ContentLength(), 24)
	}
	if string(resp.Header.ContentType()) != "aaa/bbb" {
		t.Fatalf("unexpected content-type %q. Expecting %q", resp.Header.ContentType(), "aaa/bbb")
	}

	data, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("Unexpected error when reading remaining data: %s", err)
	}
	if len(data) > 0 {
		t.Fatalf("unexpected remaining data %q", data)
	}
}

func TestServerExpect100Continue(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			if !ctx.IsPost() {
				t.Errorf("unexpected method %q. Expecting POST", ctx.Method())
			}
			if string(ctx.Path()) != "/foo" {
				t.Errorf("unexpected path %q. Expecting %q", ctx.Path(), "/foo")
			}
			ct := ctx.Request.Header.ContentType()
			if string(ct) != "a/b" {
				t.Errorf("unexpectected content-type: %q. Expecting %q", ct, "a/b")
			}
			if string(ctx.PostBody()) != "12345" {
				t.Errorf("unexpected body: %q. Expecting %q", ctx.PostBody(), "12345")
			}
			ctx.WriteString("foobar") //nolint:errcheck
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("POST /foo HTTP/1.1\r\nHost: gle.com\r\nExpect: 100-continue\r\nContent-Length: 5\r\nContent-Type: a/b\r\n\r\n12345")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, StatusOK, string(defaultContentType), "foobar")

	data, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("Unexpected error when reading remaining data: %s", err)
	}
	if len(data) > 0 {
		t.Fatalf("unexpected remaining data %q", data)
	}
}

func TestRequestCtxWriteString(t *testing.T) {
	t.Parallel()

	var ctx Ctx
	n, err := ctx.WriteString("foo")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if n != 3 {
		t.Fatalf("unexpected n %d. Expecting 3", n)
	}
	n, err = ctx.WriteString("привет")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if n != 12 {
		t.Fatalf("unexpected n=%d. Expecting 12", n)
	}

	s := ctx.Response.Body()
	if string(s) != "fooпривет" {
		t.Fatalf("unexpected response body %q. Expecting %q", s, "fooпривет")
	}
}

func TestServerConnectionClose(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			ctx.SetConnectionClose()
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")
	rw.r.WriteString("GET /must/be/ignored HTTP/1.1\r\nHost: aaa.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	var resp Response

	if err := resp.Read(br); err != nil {
		t.Fatalf("Unexpected error when parsing response: %s", err)
	}
	if !resp.ConnectionClose() {
		t.Fatal("expecting Connection: close header")
	}

	data, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("Unexpected error when reading remaining data: %s", err)
	}
	if len(data) != 0 {
		t.Fatalf("Unexpected data read after the first response %q. Expecting %q", data, "")
	}
}

func TestServerRequestNumAndTime(t *testing.T) {
	t.Parallel()

	n := uint64(0)
	var connT time.Time
	s := &Server{
		Handler: func(ctx *Ctx) {
			n++
			if ctx.ConnRequestNum() != n {
				t.Errorf("unexpected request number: %d. Expecting %d", ctx.ConnRequestNum(), n)
			}
			if connT.IsZero() {
				connT = ctx.ConnTime()
			}
			if ctx.ConnTime() != connT {
				t.Errorf("unexpected serve conn time: %s. Expecting %s", ctx.ConnTime(), connT)
			}
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")
	rw.r.WriteString("GET /bar HTTP/1.1\r\nHost: google.com\r\n\r\n")
	rw.r.WriteString("GET /baz HTTP/1.1\r\nHost: google.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	if n != 3 {
		t.Fatalf("unexpected number of requests served: %d. Expecting %d", n, 3)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, string(defaultContentType), "")
}

func TestServerEmptyResponse(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, string(defaultContentType), "")
}

func TestServerRemoteAddr(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			h := &ctx.Request.Header
			ctx.Success("text/html", []byte(fmt.Sprintf("requestURI=%s, remoteAddr=%s, remoteIP=%s",
				h.RequestURI(), ctx.RemoteAddr(), ctx.RemoteIP())))
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")

	rwx := &readWriterRemoteAddr{
		rw: rw,
		addr: &net.TCPAddr{
			IP:   []byte{1, 2, 3, 4},
			Port: 8765,
		},
	}

	if err := s.ServeConn(rwx); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, "text/html", "requestURI=/foo1, remoteAddr=1.2.3.4:8765, remoteIP=1.2.3.4")
}

func TestServerCustomRemoteAddr(t *testing.T) {
	t.Parallel()

	customRemoteAddrHandler := func(h Handler) Handler {
		return func(ctx *Ctx) {
			ctx.SetRemoteAddr(&net.TCPAddr{
				IP:   []byte{1, 2, 3, 5},
				Port: 0,
			})
			h(ctx)
		}
	}

	s := &Server{
		Handler: customRemoteAddrHandler(func(ctx *Ctx) {
			h := &ctx.Request.Header
			ctx.Success("text/html", []byte(fmt.Sprintf("requestURI=%s, remoteAddr=%s, remoteIP=%s",
				h.RequestURI(), ctx.RemoteAddr(), ctx.RemoteIP())))
		}),
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo1 HTTP/1.1\r\nHost: google.com\r\n\r\n")

	rwx := &readWriterRemoteAddr{
		rw: rw,
		addr: &net.TCPAddr{
			IP:   []byte{1, 2, 3, 4},
			Port: 8765,
		},
	}

	if err := s.ServeConn(rwx); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, "text/html", "requestURI=/foo1, remoteAddr=1.2.3.5:0, remoteIP=1.2.3.5")
}

type readWriterRemoteAddr struct {
	net.Conn
	rw   io.ReadWriteCloser
	addr net.Addr
}

func (rw *readWriterRemoteAddr) Close() error {
	return rw.rw.Close()
}

func (rw *readWriterRemoteAddr) Read(b []byte) (int, error) {
	return rw.rw.Read(b)
}

func (rw *readWriterRemoteAddr) Write(b []byte) (int, error) {
	return rw.rw.Write(b)
}

func (rw *readWriterRemoteAddr) RemoteAddr() net.Addr {
	return rw.addr
}

func (rw *readWriterRemoteAddr) LocalAddr() net.Addr {
	return rw.addr
}

func TestServerConnError(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			ctx.Error("foobar", 423)
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo/bar?baz HTTP/1.1\r\nHost: google.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	var resp Response
	if err := resp.Read(br); err != nil {
		t.Fatalf("Unexpected error when reading response: %s", err)
	}
	if resp.Header.StatusCode() != 423 {
		t.Fatalf("Unexpected status code %d. Expected %d", resp.Header.StatusCode(), 423)
	}
	if resp.Header.ContentLength() != 6 {
		t.Fatalf("Unexpected Content-Length %d. Expected %d", resp.Header.ContentLength(), 6)
	}
	if !bytes.Equal(resp.Header.Peek(HeaderContentType), defaultContentType) {
		t.Fatalf("Unexpected Content-Type %q. Expected %q", resp.Header.Peek(HeaderContentType), defaultContentType)
	}
	if !bytes.Equal(resp.Body(), []byte("foobar")) {
		t.Fatalf("Unexpected body %q. Expected %q", resp.Body(), "foobar")
	}
}

func TestServeConnSingleRequest(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			h := &ctx.Request.Header
			ctx.Success("aaa", []byte(fmt.Sprintf("requestURI=%s, host=%s", h.RequestURI(), h.Peek(HeaderHost))))
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo/bar?baz HTTP/1.1\r\nHost: google.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, "aaa", "requestURI=/foo/bar?baz, host=google.com")
}

func TestServeConnMultiRequests(t *testing.T) {
	t.Parallel()

	s := &Server{
		Handler: func(ctx *Ctx) {
			h := &ctx.Request.Header
			ctx.Success("aaa", []byte(fmt.Sprintf("requestURI=%s, host=%s", h.RequestURI(), h.Peek(HeaderHost))))
		},
	}

	rw := &readWriter{}
	rw.r.WriteString("GET /foo/bar?baz HTTP/1.1\r\nHost: google.com\r\n\r\nGET /abc HTTP/1.1\r\nHost: foobar.com\r\n\r\n")

	if err := s.ServeConn(rw); err != nil {
		t.Fatalf("Unexpected error from serveConn: %s", err)
	}

	br := bufio.NewReader(&rw.w)
	verifyResponse(t, br, 200, "aaa", "requestURI=/foo/bar?baz, host=google.com")
	verifyResponse(t, br, 200, "aaa", "requestURI=/abc, host=foobar.com")
}

func TestIncompleteBodyReturnsUnexpectedEOF(t *testing.T) {
	t.Parallel()

	rw := &readWriter{}
	rw.r.WriteString("POST /foo HTTP/1.1\r\nHost: google.com\r\nContent-Length: 5\r\n\r\n123")
	s := &Server{
		Handler: func(ctx *Ctx) {},
	}
	require.ErrorIs(t, s.ServeConn(rw), io.ErrUnexpectedEOF)
}

func verifyResponse(t *testing.T, r *bufio.Reader, expectedStatusCode int, expectedContentType, expectedBody string) *Response {
	t.Helper()
	var resp Response
	if err := resp.Read(r); err != nil {
		t.Fatalf("Unexpected error when parsing response: %s", err)
	}

	if !bytes.Equal(resp.Body(), []byte(expectedBody)) {
		t.Fatalf("Unexpected body %q. Expected %q", resp.Body(), []byte(expectedBody))
	}
	verifyResponseHeader(t, &resp.Header, expectedStatusCode, len(resp.Body()), expectedContentType)
	return &resp
}

type readWriter struct {
	net.Conn
	r bytes.Buffer
	w bytes.Buffer
}

func (rw *readWriter) Close() error {
	return nil
}

func (rw *readWriter) Read(b []byte) (int, error) {
	return rw.r.Read(b)
}

func (rw *readWriter) Write(b []byte) (int, error) {
	return rw.w.Write(b)
}

func (rw *readWriter) RemoteAddr() net.Addr {
	return zeroTCPAddr
}

func (rw *readWriter) LocalAddr() net.Addr {
	return zeroTCPAddr
}

func (rw *readWriter) SetDeadline(t time.Time) error {
	return nil
}

func (rw *readWriter) SetReadDeadline(t time.Time) error {
	return nil
}

func (rw *readWriter) SetWriteDeadline(t time.Time) error {
	return nil
}
