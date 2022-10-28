package rpcgateway

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"io"
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

func (rpc *RPCGateway) LoggingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		fn := func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					zap.L().Error("internal server error while processing request", zap.Any("error", err), zap.Any("trace", debug.Stack()))
				}
			}()

			recorder := NewHTTPStatusRecorder(w)

			fields := []zap.Field{
				zap.String("path", r.URL.EscapedPath()),
				zap.String("method", r.Method),
				zap.Int("statusCode", recorder.status),
			}

			start := time.Now()

			if rpc.config.Logging.LogRequestBody {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					zap.L().Error("cannot read request body", zap.Error(err))

					w.WriteHeader(http.StatusInternalServerError)

					return
				}

				var data bytes.Buffer
				gz := gzip.NewWriter(&data)
				if _, err := gz.Write(body); err != nil {
					zap.L().Error("cannot compress data", zap.Error(err))

					w.WriteHeader(http.StatusInternalServerError)

					return
				}
				if err := gz.Close(); err != nil {
					zap.L().Error("cannot close gzip", zap.Error(err))

					w.WriteHeader(http.StatusInternalServerError)

					return
				}

				fields = append(fields,
					zap.String("body", base64.StdEncoding.EncodeToString(data.Bytes())))

				reader := io.NopCloser(bytes.NewBuffer(body))
				r.Body = reader
			}

			next.ServeHTTP(recorder, r)

			fields = append(fields, zap.Duration("duration", time.Since(start)))

			zap.L().Info("processed request", fields...)
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
