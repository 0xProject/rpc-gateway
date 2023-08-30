package rpcgateway

import (
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

type HTTPStatusRecorder struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func NewHTTPStatusRecorder(w http.ResponseWriter) *HTTPStatusRecorder {
	return &HTTPStatusRecorder{ResponseWriter: w}
}

func (r *HTTPStatusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}

	r.status = status
	r.ResponseWriter.WriteHeader(status)
	r.wroteHeader = true
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
			recorder := NewHTTPStatusRecorder(w)
			next.ServeHTTP(recorder, r)

			zap.L().Info("processed request",
				zap.String("path", r.URL.EscapedPath()),
				zap.String("method", r.Method),
				zap.Int("statusCode", recorder.status),
				zap.Int64("duration", int64(time.Since(start))))
		}

		return http.HandlerFunc(fn)
	}
}

func RequestCounters(c *prometheus.CounterVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			recorder := NewHTTPStatusRecorder(w)
			next.ServeHTTP(recorder, r)

			labels := prometheus.Labels{
				"status_code": fmt.Sprintf("%d", recorder.status),
				"method":      r.Method,
			}

			c.With(labels).Inc()
		}
		return http.HandlerFunc(fn)
	}
}
