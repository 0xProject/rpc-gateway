package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Proxy struct {
	config             Config
	targets            []*NodeProvider
	healthcheckManager *HealthCheckManager

	metricResponseTime   *prometheus.HistogramVec
	metricRequestErrors  *prometheus.CounterVec
	metricResponseStatus *prometheus.CounterVec
}

func NewProxy(proxyConfig Config, healthCheckManager *HealthCheckManager) *Proxy {
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
		p, err := NewNodeProvider(target)
		if err != nil {
			// TODO
			// Remove a call to panic()
			//
			panic(err)
		}

		proxy.targets = append(proxy.targets, p)
	}

	return proxy
}

func (p *Proxy) HasNodeProviderFailed(statusCode int) bool {
	return statusCode >= http.StatusInternalServerError || statusCode == http.StatusTooManyRequests
}

func (p *Proxy) copyHeaders(dst http.ResponseWriter, src http.ResponseWriter) {
	for k, v := range src.Header() {
		if len(v) == 0 {
			continue
		}

		dst.Header().Set(k, v[0])
	}
}

func (p *Proxy) timeoutHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		http.TimeoutHandler(next,
			p.config.Proxy.UpstreamTimeout,
			http.StatusText(http.StatusGatewayTimeout)).ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func (p *Proxy) errServiceUnavailable(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body := &bytes.Buffer{}

	if _, err := io.Copy(body, r.Body); err != nil {
		p.errServiceUnavailable(w)

		return
	}

	for _, target := range p.targets {
		if !p.healthcheckManager.IsHealthy(target.Config.Name) {
			continue
		}
		start := time.Now()

		pw := NewResponseWriter()
		r.Body = io.NopCloser(bytes.NewBuffer(body.Bytes()))

		p.timeoutHandler(target).ServeHTTP(pw, r)

		if p.HasNodeProviderFailed(pw.statusCode) {
			p.metricResponseTime.WithLabelValues(target.Config.Name, r.Method).Observe(time.Since(start).Seconds())
			p.metricResponseStatus.WithLabelValues(target.Config.Name, strconv.Itoa(pw.statusCode)).Inc()
			p.metricRequestErrors.WithLabelValues(target.Config.Name, "rerouted").Inc()

			continue
		}
		p.copyHeaders(w, pw)

		w.WriteHeader(pw.statusCode)
		w.Write(pw.body.Bytes()) // nolint:errcheck

		p.metricResponseStatus.WithLabelValues(target.Config.Name, strconv.Itoa(pw.statusCode)).Inc()
		p.metricResponseTime.WithLabelValues(target.Config.Name, r.Method).Observe(time.Since(start).Seconds())

		return
	}

	p.errServiceUnavailable(w)
}
