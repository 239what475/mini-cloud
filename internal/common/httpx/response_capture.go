package httpx

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

type StatusCapturingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *StatusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *StatusCapturingResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *StatusCapturingResponseWriter) Flush() {
	flusher, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	flusher.Flush()
}

func (w *StatusCapturingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func (w *StatusCapturingResponseWriter) Push(target string, opts *http.PushOptions) error {
	pusher, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return pusher.Push(target, opts)
}

func (w *StatusCapturingResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	readerFrom, ok := w.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(w.ResponseWriter, r)
	}
	return readerFrom.ReadFrom(r)
}

func (w *StatusCapturingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
