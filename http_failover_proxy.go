package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"time"

	"go.uber.org/zap"
)

type HTTPTarget struct {
	Config TargetConfig
	Proxy  *httputil.ReverseProxy
}

type HTTPFailoverProxy struct {
	gatewayConfig      RPCGatewayConfig
	targets            []*HTTPTarget
	healthcheckManager *HealthcheckManager
}

func NewHTTPFailoverProxy(config RPCGatewayConfig, healthCheckManager *HealthcheckManager) *HTTPFailoverProxy {
	proxy := &HTTPFailoverProxy{
		gatewayConfig:      config,
		healthcheckManager: healthCheckManager,
	}

	for index, target := range config.Targets {
		if err := proxy.AddTarget(target, uint(index)); err != nil {
			panic(err)
		}
	}

	return proxy
}

func (h *HTTPFailoverProxy) doModifyResponse(config TargetConfig) func(*http.Response) error {
	return func(resp *http.Response) error {
		responseStatus.WithLabelValues(config.Name, strconv.Itoa(resp.StatusCode)).Inc()

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

func (h *HTTPFailoverProxy) doErrorHandler(proxy *httputil.ReverseProxy, config TargetConfig, index uint) func(http.ResponseWriter, *http.Request, error) {
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
		if retries < h.gatewayConfig.Proxy.AllowedNumberOfRetriesPerTarget {
			requestErrorsHandled.WithLabelValues(config.Name, "retry").Inc()
			// we add a configurable delay before resending request
			//
			<-time.After(h.gatewayConfig.Proxy.RetryDelay)

			ctx := context.WithValue(r.Context(), Retries, retries+1)
			proxy.ServeHTTP(w, r.WithContext(ctx))

			return
		}

		// route the request to a different target
		requestErrorsHandled.WithLabelValues(config.Name, "rerouted").Inc()
		reroutes := GetReroutesFromContext(r)
		visitedTargets := GetVisitedTargetsFromContext(r)
		ctx := context.WithValue(r.Context(), Reroutes, reroutes+1)

		// add the current target to the VisitedTargets slice to exclude it when selecting
		// the next target
		ctx = context.WithValue(ctx, VisitedTargets, append(visitedTargets, index))

		// adding the targetname in case it errors out and needs to be
		// used in metrics in ServeHTTP.
		ctx = context.WithValue(ctx, TargetName, config.Name)

		// reset the number of retries for the next target
		ctx = context.WithValue(ctx, Retries, 0)

		h.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (h *HTTPFailoverProxy) AddTarget(config TargetConfig, index uint) error {
	proxy, err := NewPathPreservingProxy(config, h.gatewayConfig.Proxy)
	if err != nil {
		return err
	}

	// NOTE: any error returned from ModifyResponse will be handled by
	// ErrorHandler
	// proxy.ModifyResponse = h.doModifyResponse(config)
	//
	proxy.ModifyResponse = h.doModifyResponse(config)
	proxy.ErrorHandler = h.doErrorHandler(proxy, config, index)

	h.targets = append(
		h.targets,
		&HTTPTarget{
			Config: config,
			Proxy:  proxy,
		})

	return nil
}

func (h *HTTPFailoverProxy) GetNextTarget() *HTTPTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndex()

	return h.targets[idx]
}

func (h *HTTPFailoverProxy) GetNextTargetExcluding(indexes []uint) *HTTPTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndexExcluding(indexes)

	return h.targets[idx]
}

func (h *HTTPFailoverProxy) GetNextTargetName() string {
	return h.GetNextTarget().Config.Name
}

func (h *HTTPFailoverProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reroutes := GetReroutesFromContext(r)
	if reroutes > h.gatewayConfig.Proxy.AllowedNumberOfReroutes {
		targetName := GetTargetNameFromContext(r)
		zap.L().Warn("request reached maximum reroutes", zap.String("remoteAddr", r.RemoteAddr), zap.String("url", r.URL.Path))
		requestErrorsHandled.WithLabelValues(targetName, "failure").Inc()

		http.Error(w, "Service not available", http.StatusServiceUnavailable)
		return
	}

	visitedTargets := GetVisitedTargetsFromContext(r)

	peer := h.GetNextTargetExcluding(visitedTargets)
	if peer != nil {
		start := time.Now()
		peer.Proxy.ServeHTTP(w, r)
		duration := time.Since(start)
		responseTimeHistogram.WithLabelValues(peer.Config.Name, r.Method).Observe(duration.Seconds())
		return
	}

	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}
