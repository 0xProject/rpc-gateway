package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type MetricsServer struct {
	server *http.Server
}

func (h *MetricsServer) Start() error {
	zap.L().Info("metrics server starting", zap.String("listenAddr", h.server.Addr))
	return h.server.ListenAndServe()
}

func (h *MetricsServer) Stop() error {
	return h.server.Close()
}

func NewMetricsServer(config MetricsConfig) *MetricsServer {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Handler:      mux,
		Addr:         fmt.Sprintf(":%s", config.Port),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	return &MetricsServer{
		server: srv,
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "{\"healthy\":true}")
}
