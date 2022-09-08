package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
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
					.005,
					.01,
					.025,
					.05,
					.1,
					.25,
					.5,
					1,
					2.5,
					5,
					10,
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

	for index, target := range proxy.config.Targets {
		if err := proxy.AddTarget(target, uint(index)); err != nil {
			panic(err)
		}
	}

	return proxy
}

func (h *Proxy) doModifyResponse(config TargetConfig) func(*http.Response) error {
	return func(resp *http.Response) error {
		h.metricResponseStatus.WithLabelValues(config.Name, strconv.Itoa(resp.StatusCode)).Inc()

		switch {
		// Here's the thing. A different provider may response with a
		// different status code for the same query.  e.g. call for
		// a block that does not exist, Alchemy will serve HTTP 400
		// where Infura will serve HTTP 200.  Both of these responses
		// hold a concrete error in jsonrpc message.
		//
		// Having this in mind, we may consider a provider unreliable
		// upon these events:
		//  - HTTP 5xx responses
		//  - Cannot make a connection after X of retries.
		//
		// Everything else, as long as it's jsonrpc payload should be
		// considered as successful response.
		//
		case resp.StatusCode == http.StatusTooManyRequests:
			// this code generates a fallback to backup provider.
			//
			zap.L().Warn("rate limited", zap.String("provider", config.Name))

			return errors.New("rate limited")

		case resp.StatusCode >= http.StatusInternalServerError:
			// this code generates a fallback to backup provider.
			//
			zap.L().Warn("server error", zap.String("provider", config.Name))

			return errors.New("server error")
		default:
			h.healthcheckManager.ObserveSuccess(config.Name)
		}

		return nil
	}
}

func (h *Proxy) doErrorHandler(proxy *httputil.ReverseProxy, config TargetConfig, index uint) func(http.ResponseWriter, *http.Request, error) {
	return func(w http.ResponseWriter, r *http.Request, e error) {
		// The client canceled the request (e.g. 0x API has a 5s timeout for RPC request)
		// we stop here as it doesn't make sense to retry/reroute anymore.
		// Also, we don't want to observe a client-canceled request as a failure
		if errors.Is(e, context.Canceled) {
			return
		}

		retries := GetRetryFromContext(r)

		// Workaround to reserve request body in ReverseProxy.ErrorHandler see
		// more here: https://github.com/golang/go/issues/33726
		//
		if buf, ok := r.Context().Value("bodybuf").(*bytes.Buffer); ok {
			r.Body = io.NopCloser(buf)
		}

		zap.L().Warn("handling a failed request", zap.String("provider", config.Name), zap.Error(e))
		h.healthcheckManager.ObserveFailure(config.Name)
		if retries < h.config.Proxy.AllowedNumberOfRetriesPerTarget {
			h.metricRequestErrors.WithLabelValues(config.Name, "retry").Inc()
			// we add a configurable delay before resending request
			//
			<-time.After(h.config.Proxy.RetryDelay)

			ctx := context.WithValue(r.Context(), Retries, retries+1)
			proxy.ServeHTTP(w, r.WithContext(ctx))

			return
		}

		// route the request to a different target
		h.metricRequestErrors.WithLabelValues(config.Name, "rerouted").Inc()
		visitedTargets := GetVisitedTargetsFromContext(r)

		// add the current target to the VisitedTargets slice to exclude it when selecting
		// the next target
		ctx := context.WithValue(r.Context(), VisitedTargets, append(visitedTargets, index))

		// adding the targetname in case it errors out and needs to be
		// used in metrics in ServeHTTP.
		ctx = context.WithValue(ctx, TargetName, config.Name)

		// reset the number of retries for the next target
		ctx = context.WithValue(ctx, Retries, 0)

		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (h *Proxy) AddTarget(target TargetConfig, index uint) error {
	proxy, err := NewReverseProxy(target, h.config)
	if err != nil {
		return err
	}

	// NOTE: any error returned from ModifyResponse will be handled by
	// ErrorHandler
	// proxy.ModifyResponse = h.doModifyResponse(config)
	//
	proxy.ModifyResponse = h.doModifyResponse(target) // nolint:bodyclose
	proxy.ErrorHandler = h.doErrorHandler(proxy, target, index)

	h.targets = append(
		h.targets,
		&HTTPTarget{
			Config: target,
			Proxy:  proxy,
		})

	return nil
}

func (h *Proxy) GetNextTarget() *HTTPTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndex()

	return h.targets[idx]
}

func (h *Proxy) GetNextTargetExcluding(indexes []uint) *HTTPTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndexExcluding(indexes)

	return h.targets[idx]
}

func (h *Proxy) GetNextTargetName() string {
	return h.GetNextTarget().Config.Name
}

func (h *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	visitedTargets := GetVisitedTargetsFromContext(r)

	peer := h.GetNextTargetExcluding(visitedTargets)
	if peer != nil {
		start := time.Now()
		peer.Proxy.ServeHTTP(w, r)
		duration := time.Since(start)
		h.metricResponseTime.WithLabelValues(peer.Config.Name, r.Method).Observe(duration.Seconds())

		return
	}

	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}
