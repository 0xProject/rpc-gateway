package metrics

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	server *echo.Echo
}

func (s *Server) Start() error {
	return s.server.Start(":3000")
}

func (s *Server) Stop() error {
	return s.server.Close()
}

func NewServer(config Config) *Server {
	server := echo.New()
	server.HideBanner = true

	server.GET("/healthz", func(c echo.Context) error {
		return c.String(http.StatusOK, "{ \"healthy\": true }")
	})
	server.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	return &Server{
		server: server,
	}
}
