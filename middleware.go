package main

import (
	"net/http"

	"go.uber.org/zap"
)

type RequestLogger struct {
}

func (rl *RequestLogger) Init(config map[string]interface{}) error {
	return nil
}

func (rl *RequestLogger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsProcessed.Inc()
		ip := ReadUserIP(r)
		zap.L().Info("received request", zap.String("requestURI", r.RequestURI), zap.String("sourceIP", ip))
		next.ServeHTTP(w, r)
	})
}
