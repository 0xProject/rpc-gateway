package main

import (
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type responseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func wrapResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}

	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
	rw.wroteHeader = true

	return
}

// LoggingMiddleware logs the incoming HTTP request & its duration
// and also report the metrics to prometheus.
func LoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					zap.L().Error("internal server error while processing request", zap.Any("error", err), zap.Any("trace", debug.Stack()))
				}
			}()

			start := time.Now()
			wrapped := wrapResponseWriter(w)
			next.ServeHTTP(wrapped, r)
			requestsProcessed.WithLabelValues(strconv.Itoa(wrapped.status)).Inc()
			zap.L().Info("processed request", zap.String("path", r.URL.EscapedPath()), zap.String("method", r.Method), zap.Int("statusCode", wrapped.status), zap.Int64("duration", int64(time.Since(start))))
		}

		return http.HandlerFunc(fn)
	}
}
