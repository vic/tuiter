package main

import (
	"log"
	"net/http"
	"strings"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.status = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (lrw *loggingResponseWriter) Write(b []byte) (int, error) {
	if lrw.status == 0 {
		lrw.status = http.StatusOK
	}
	n, err := lrw.ResponseWriter.Write(b)
	lrw.bytes += n
	return n, err
}

func redactHeaders(h http.Header) http.Header {
	out := make(http.Header)
	for k, vv := range h {
		if strings.EqualFold(k, "Authorization") || strings.EqualFold(k, "Cookie") {
			out[k] = []string{"<redacted>"}
			continue
		}
		out[k] = vv
	}
	return out
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		headers := redactHeaders(r.Header)
		log.Printf("DEBUG: Incoming request - method=%s url=%s remote=%s headers=%v", r.Method, r.URL.String(), r.RemoteAddr, headers)

		lrw := &loggingResponseWriter{ResponseWriter: w}
		next.ServeHTTP(lrw, r)

		if lrw.status == 0 {
			lrw.status = http.StatusOK
		}
		duration := time.Since(start)
		log.Printf("DEBUG: Response completed - method=%s url=%s status=%d bytes=%d duration=%s", r.Method, r.URL.String(), lrw.status, lrw.bytes, duration)
	})
}
