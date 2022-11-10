package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/pkg/errors"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Proxy struct {
	config             Config
	targets            []*HTTPTarget
	healthcheckManager *HealthcheckManager

	metricResponseTime   *prometheus.HistogramVec
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
		metricResponseStatus: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "zeroex_rpc_gateway_target_response_status_total",
				Help: "Total number of responses with a statuscode label",
			}, []string{
				"provider",
				"status_code",
			}),
	}

	for _, target := range proxy.config.Targets {
		t := &HTTPTarget{
			Config: target,
			ClientOptions: HTTPTargetClientOptions{
				Timeout: proxy.config.Proxy.UpstreamTimeout,
			},
		}
		proxy.targets = append(proxy.targets, t)
	}

	return proxy
}

func (h *Proxy) hasError(r *http.Response) bool {
	return r.StatusCode == http.StatusTooManyRequests || r.StatusCode >= http.StatusInternalServerError
}

func (h *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// You cannot read request body more than once. In case you do without
	// preserving the copy, you will find yourself with empty body.
	// Bazinga.
	//
	body := &bytes.Buffer{}
	if _, err := io.Copy(body, r.Body); err != nil {
		http.Error(w,
			http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}

	for _, target := range h.targets {
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}

		if !h.healthcheckManager.IsTargetHealthy(target.Config.Name) {
			continue
		}

		c, cancel := context.WithTimeout(context.Background(), h.config.Proxy.UpstreamTimeout)
		defer cancel()

		r.Body = io.NopCloser(bytes.NewBuffer(body.Bytes()))

		resp, err := target.Do(c, r)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		h.metricResponseStatus.WithLabelValues(
			target.Config.Name, strconv.Itoa(resp.StatusCode)).Inc()

		if h.hasError(resp) {
			continue
		}

		if _, err := io.Copy(w, resp.Body); err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}

		return
	}

	http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
}
