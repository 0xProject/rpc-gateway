package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"time"

	"github.com/0xProject/rpc-gateway/internal/middleware"
	"github.com/go-http-utils/headers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type HTTPTarget struct {
	Config TargetConfig
	Proxy  *httputil.ReverseProxy
}

type Proxy struct {
	config             Config
	targets            []*HTTPTarget
	healthcheckManager *HealthcheckManager

	metricResponseTime   *prometheus.HistogramVec
	metricRequestErrors  *prometheus.CounterVec
	metricResponseStatus *prometheus.CounterVec
}

func NewProxy(proxyConfig Config, healthCheckManager *HealthcheckManager) *Proxy {
	proxy := &Proxy{
		config:             proxyConfig,
		healthcheckManager: healthCheckManager,
		metricResponseTime: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "zeroex_rpc_gateway_request_duration_seconds",
				Help: "Histogram of response time for Gateway in seconds",
				Buckets: []float64{
					.025,
					.05,
					.1,
					.25,
					.5,
					1,
					2.5,
					5,
					10,
					15,
					20,
					25,
					30,
				},
			}, []string{
				"provider",
				"method",
			}),
		metricRequestErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "zeroex_rpc_gateway_request_errors_handled_total",
				Help: "The total number of request errors handled by gateway",
			}, []string{
				"provider",
				"type",
			}),
		metricResponseStatus: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "zeroex_rpc_gateway_target_response_status_total",
			Help: "Total number of responses with a statuscode label",
		}, []string{
			"provider",
			"status_code",
		}),
	}

	for _, target := range proxy.config.Targets {
		if err := proxy.AddTarget(target); err != nil {
			panic(err)
		}
	}

	return proxy
}

func (h *Proxy) AddTarget(target TargetConfig) error {
	proxy, err := NewReverseProxy(target, h.config)
	if err != nil {
		return err
	}

	h.targets = append(
		h.targets,
		&HTTPTarget{
			Config: target,
			Proxy:  proxy,
		})

	return nil
}

func (h *Proxy) HasNodeProviderFailed(statusCode int) bool {
	return statusCode >= http.StatusInternalServerError || statusCode == http.StatusTooManyRequests
}

func (h *Proxy) copyHeaders(dst http.ResponseWriter, src http.ResponseWriter) {
	for k, v := range src.Header() {
		if len(v) == 0 {
			continue
		}

		dst.Header().Set(k, v[0])
	}
}

func (h *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := &bytes.Buffer{}

	if _, err := io.Copy(body, r.Body); err != nil {
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
	}

	for _, target := range h.targets {
		start := time.Now()

		pw := NewResponseWriter()
		r.Body = io.NopCloser(bytes.NewBuffer(body.Bytes()))

		if !target.Config.Connection.HTTP.Compression && strings.Contains(r.Header.Get(headers.ContentEncoding), "gzip") {
			middleware.Gunzip(target.Proxy).ServeHTTP(pw, r)
		} else {
			target.Proxy.ServeHTTP(pw, r)
		}

		if h.HasNodeProviderFailed(pw.statusCode) {
			h.metricResponseTime.WithLabelValues(target.Config.Name, r.Method).Observe(time.Since(start).Seconds())
			h.metricRequestErrors.WithLabelValues(target.Config.Name, "rerouted").Inc()

			continue
		}
		h.copyHeaders(w, pw)

		w.WriteHeader(pw.statusCode)
		w.Write(pw.body.Bytes()) // nolint:errcheck

		h.metricResponseTime.WithLabelValues(target.Config.Name, r.Method).Observe(time.Since(start).Seconds())

		return
	}

	http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
}
