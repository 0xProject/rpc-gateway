package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"strconv"
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
	for targetIndex, httpTarget := range config.Targets {
		if err := proxy.AddHttpTarget(httpTarget, uint(targetIndex)); err != nil {
			panic(err)
		}
	}

	return proxy
}

func (h *HttpFailoverProxy) addTarget(target *HttpTarget) {
	h.targets = append(h.targets, target)
}

func (h *HttpFailoverProxy) AddHttpTarget(targetConfig TargetConfig, targetIndex uint) error {
	targetURL := targetConfig.Connection.HTTP.URL
	targetName := targetConfig.Name

	proxy, err := NewPathPreservingProxy(targetURL, h.gatewayConfig.Proxy)
	if err != nil {
		return err
	}

	// NOTE: any error returned from ModifyResponse will be handled by
	// ErrorHandler
	proxy.ModifyResponse = func(response *http.Response) error {
		responseStatus.WithLabelValues(targetName, strconv.Itoa(response.StatusCode)).Inc()
		if response.StatusCode == 429 {
			zap.L().Warn("rate limited", zap.String("target", targetName))
			return errors.New("rate limited")
		} else if response.StatusCode >= 300 {
			zap.L().Warn("received a non succesful status code", zap.Int("statusCode", response.StatusCode))
			return fmt.Errorf("status code: %d", response.StatusCode)
		} else {
			h.healthcheckManager.ObserveSuccess(targetName)
		}

		return nil
	}

	proxy.ErrorHandler = func(writer http.ResponseWriter, request *http.Request, e error) {
		retries := GetRetryFromContext(request)
		zap.L().Warn("handling a failed request", zap.Error(e))
		h.healthcheckManager.ObserveFailure(targetName)
		if retries < h.gatewayConfig.Proxy.AllowedNumberOfRetriesPerTarget {
			requestErrorsHandled.WithLabelValues(targetName, "retry").Inc()
			// we add a configurable delay before resending request
			time.Sleep(h.gatewayConfig.Proxy.RetryDelay)
			ctx := context.WithValue(request.Context(), Retries, retries+1)
			proxy.ServeHTTP(writer, request.WithContext(ctx))
			return
		}

		// route the request to a different target
		requestErrorsHandled.WithLabelValues(targetName, "rerouted").Inc()
		reroutes := GetReroutesFromContext(request)
		visitedTargets := GetVisitedTargetsFromContext(request)
		ctx := context.WithValue(request.Context(), Reroutes, reroutes+1)

		// add the current target to the VisitedTargets slice to exclude it when selecting
		// the next target
		ctx = context.WithValue(ctx, VisitedTargets, append(visitedTargets, targetIndex))

		// adding the targetname in case it errors out and needs to be
		// used in metrics in ServeHTTP.
		ctx = context.WithValue(ctx, TargetName, targetName)
		
		// reset the number of retries for the next target
		ctx = context.WithValue(ctx, Retries, 0)
		h.ServeHTTP(writer, request.WithContext(ctx))
	}

	target := &HttpTarget{
		Config: targetConfig,
		Proxy:  proxy,
	}

	h.addTarget(target)

	return nil
}

func (h *HttpFailoverProxy) GetNextTarget() *HttpTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndex()
	return h.targets[idx]
}

func (h *HttpFailoverProxy) GetNextTargetExcluding(indexes []uint) *HttpTarget {
	idx := h.healthcheckManager.GetNextHealthyTargetIndexExcluding(indexes)
	return h.targets[idx]
}

func (h *HttpFailoverProxy) GetNextTargetName() string {
	return h.GetNextTarget().Config.Name
}

func (h *HttpFailoverProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
