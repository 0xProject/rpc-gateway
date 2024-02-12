package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

type Server struct {
	server *http.Server
}

func (s *Server) Start() error {
	zap.L().Info("metrics server starting", zap.String("listenAddr", s.server.Addr))
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	return s.server.Close()
}

func NewServer(config Config) *Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "{\"healthy\":true}")
	})
	mux.Handle("/metrics", promhttp.Handler())

	return &Server{
		server: &http.Server{
			Handler:           mux,
			Addr:              fmt.Sprintf(":%d", config.Port),
			WriteTimeout:      15 * time.Second,
			ReadTimeout:       15 * time.Second,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}
}
