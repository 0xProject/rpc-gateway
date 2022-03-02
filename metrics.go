package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (

	//requestDurations = ""
	requestBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

	responseTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "zeroex_rpc_gateway_request_duration_seconds",
		Help:    "Histogram of response time for Gateway in seconds",
		Buckets: requestBuckets,
	}, []string{"host", "method"})

	gatewayFailover = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "zeroex_rpc_gateway_http_failover_index",
		Help: "Index of the currently selected HTTP target",
	})

	rpcProviderBlockNumber = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "zeroex_rpc_gateway_provider_block_number",
		Help: "Block number of a given provider",
	}, []string{"provider"})

	rpcProviderGasLimit = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "zeroex_rpc_gateway_provider_gasLimit_number",
		Help: "Gas limit of a given provider",
	}, []string{"provider"})

	healthcheckResponseTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "zeroex_rpc_gateway_healthcheck_response_duration_seconds",
		Help:    "Histogram of response time for Gateway Healthchecker in seconds",
		Buckets: requestBuckets,
	}, []string{"host", "method"})

	requestsProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "zeroex_rpc_gateway_requests_total",
		Help: "The total number of processed requests by gateway",
	})

	requestErrorsHandled = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "zeroex_rpc_gateway_request_errors_handled_total",
		Help: "The total number of request errors handled by gateway",
	}, []string{"host", "type"})

	responseStatus = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "zeroex_rpc_gateway_target_response_status_total",
		Help: "Total number of responses with a statuscode label",
	}, []string{"host", "status_code"})
)

func init() {
	prometheus.MustRegister(responseTimeHistogram)
	prometheus.MustRegister(gatewayFailover)
	prometheus.MustRegister(rpcProviderBlockNumber)
	prometheus.MustRegister(healthcheckResponseTimeHistogram)
	gatewayFailover.Set(float64(0))
}

type metricsServer struct {
	server *http.Server
}

func (h *metricsServer) Start() error {
	zap.L().Info("metrics server starting", zap.String("listenAddr", h.server.Addr))
	return h.server.ListenAndServe()
}

func (h *metricsServer) Stop() error {
	return h.server.Close()
}

func NewMetricsServer(config MetricsConfig) *metricsServer {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Handler:      mux,
		Addr:         fmt.Sprintf("0.0.0.0:%s", config.Port),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	return &metricsServer{
		server: srv,
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "{\"healthy\":true}")
}
