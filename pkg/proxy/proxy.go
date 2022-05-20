package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httputil"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
)

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
		healthchecker := healthCheckManager.GetTargetByName(target.Name)

		if err := proxy.AddTarget(target, healthchecker, uint(index)); err != nil {
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

		// route the request to a different target
		h.metricRequestErrors.WithLabelValues(config.Name, "rerouted").Inc()
	}
}

func (h *Proxy) AddTarget(target TargetConfig, healthchecker Healthchecker, index uint) error {
	proxy, err := NewReverseProxy(target, h.config)
	if err != nil {
		return err
	}

	// NOTE: any error returned from ModifyResponse will be handled by
	// ErrorHandler
	// proxy.ModifyResponse = h.doModifyResponse(config)
	//
	proxy.ModifyResponse = h.doModifyResponse(target)
	proxy.ErrorHandler = h.doErrorHandler(proxy, target, index)

	h.targets = append(
		h.targets,
		&HTTPTarget{
			Config:        target,
			Proxy:         proxy,
			Healthchecker: healthchecker,
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
	// reroutes := GetReroutesFromContext(r)
	// if reroutes > h.config.Proxy.AllowedNumberOfReroutes {
	// 	targetName := GetTargetNameFromContext(r)
	// 	zap.L().Warn("request reached maximum reroutes", zap.String("remoteAddr", r.RemoteAddr), zap.String("url", r.URL.Path))
	// 	h.metricRequestErrors.WithLabelValues(targetName, "failure").Inc()

	// 	http.Error(w, "Service not available", http.StatusServiceUnavailable)
	// 	return
	// }

	// visitedTargets := GetVisitedTargetsFromContext(r)

	// peer := h.GetNextTargetExcluding(visitedTargets)
	// if peer != nil {
	// 	start := time.Now()
	// 	peer.Proxy.ServeHTTP(w, r)
	// 	duration := time.Since(start)
	// 	h.metricResponseTime.WithLabelValues(peer.Config.Name, r.Method).Observe(duration.Seconds())
	// 	return
	// }

	// Here's the thing.
	// I don't believe we should track visited nodes at all. We should track
	// healthiness of a backend. It makes things easier as fuck.

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic(err)
	}

	for _, target := range h.targets {
		if !target.Healthy() {
			continue
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		if status := target.Do(w, r); status != 200 {

			fmt.Println("STATUS ", status)

			continue
		}

		return
	}

	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}
