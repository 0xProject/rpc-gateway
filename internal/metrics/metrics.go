package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	server *http.Server
}

func (s *Server) Start() error {
	return s.server.ListenAndServe()
}

func (s *Server) Stop() error {
	return s.server.Close()
}

func NewServer(config Config) *Server {
	r := chi.NewRouter()

	r.Use(middleware.Heartbeat("/healthz"))
	r.Handle("/metrics", promhttp.Handler())

	return &Server{
		server: &http.Server{
			Handler:           r,
			Addr:              fmt.Sprintf(":%d", config.Port),
			WriteTimeout:      time.Second * 15,
			ReadTimeout:       time.Second * 15,
			ReadHeaderTimeout: time.Second * 5,
		},
	}
}
