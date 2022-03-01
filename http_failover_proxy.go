package main

import (
	"context"
	"net/http"
	"net/http/httputil"
	"time"

	"go.uber.org/zap"
)

type HttpTarget struct {
	Config TargetConfig
	Proxy  *httputil.ReverseProxy
}

type HttpFailoverProxy struct {
	gatewayConfig      RpcGatewayConfig
	targets            []*HttpTarget
	healthcheckManager *HealthcheckManager
}

func NewHttpFailoverProxy(config RpcGatewayConfig, healthCheckManager *HealthcheckManager) *HttpFailoverProxy {
	proxy := &HttpFailoverProxy{
		gatewayConfig:      config,
		healthcheckManager: healthCheckManager,
	}
	for _, httpTarget := range config.Targets {
		if err := proxy.AddHttpTarget(httpTarget); err != nil {
			panic(err)
		}
	}

	return proxy
}

func (h *HttpFailoverProxy) addTarget(target *HttpTarget) {
	h.targets = append(h.targets, target)
}

func (h *HttpFailoverProxy) AddHttpTarget(targetConfig TargetConfig) error {
	targetURL := targetConfig.Connection.HTTP.URL
	targetName := targetConfig.Name

	proxy, err := NewPathPreservingProxy(targetURL, h.gatewayConfig.Proxy)
	if err != nil {
		return err
	}

	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, e error) {
		retries := GetRetryFromContext(request)
		zap.L().Warn("handling a failed request", zap.Error(e))
		if retries < h.gatewayConfig.Proxy.AllowedNumberOfRetriesPerTarget {
			requestErrorsHandled.WithLabelValues(targetName, "retry").Inc()
			// we add a configurable delay before resending request
			time.Sleep(h.gatewayConfig.Proxy.RetryDelay)
			ctx := context.WithValue(request.Context(), Retries, retries+1)
			proxy.ServeHTTP(writer, request.WithContext(ctx))
			return
		}

		// the request has failed 3 times, we mark the target as tainted.
		h.SetTargetTaint(targetName, true)
		// TODO: move into a high level taint management with blockNumbers quorum and "rate of retries" instead.
		// We remove the tain as the request rerouting could be a
		// "blip", we give a minute for the RPC provider to recover.
		// Health is still checked independently.
		go func() {
			time.Sleep(60 * time.Second)
			h.SetTargetTaint(targetName, false)
		}()

		// route the request to a different backend
		requestErrorsHandled.WithLabelValues(targetName, "rerouted").Inc()
		reroutes := GetReroutesFromContext(request)
		ctx := context.WithValue(request.Context(), Reroutes, reroutes+1)
		// adding the targetname in case it errors out and needs to be
		// used in metrics in ServeHTTP.
		ctx = context.WithValue(ctx, TargetName, targetName)
		h.ServeHTTP(writer, request.WithContext(ctx))
	}

	target := &HttpTarget{
		Config: targetConfig,
		Proxy:  proxy,
	}

	h.addTarget(target)

	return nil
}

func (h *HttpFailoverProxy) SetTargetTaint(name string, isTainted bool) {
	h.healthcheckManager.SetTargetTaint(name, isTainted)
}

func (h *HttpFailoverProxy) GetNextTarget() *HttpTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndex()
	return h.targets[idx]
}

func (h *HttpFailoverProxy) GetNextTargetName() string {
	return h.GetNextTarget().Config.Name
}

func (h *HttpFailoverProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	failovers := GetReroutesFromContext(r)
	if failovers > h.gatewayConfig.Proxy.AllowedNumberOfReroutes {
		targetName := GetTargetNameFromContext(r)
		zap.L().Warn("request reached maximum failovers", zap.String("remoteAddr", r.RemoteAddr), zap.String("url", r.URL.Path))
		requestErrorsHandled.WithLabelValues(targetName, "failure").Inc()
		http.Error(w, "Service not available", http.StatusServiceUnavailable)
		return
	}

	peer := h.GetNextTarget()
	if peer != nil {
		start := time.Now()
		peer.Proxy.ServeHTTP(w, r)
		duration := time.Since(start)
		responseTimeHistogram.WithLabelValues(peer.Config.Name, r.Method).Observe(duration.Seconds())
		return
	}
	http.Error(w, "Service not available", http.StatusServiceUnavailable)
}
