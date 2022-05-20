package proxy

import (
	"net/http"
	"net/http/httputil"
)

type HTTPTarget struct {
	Config        TargetConfig
	Healthchecker Healthchecker
	Proxy         *httputil.ReverseProxy
}

type HTTPResponeRecorder struct {
	http.ResponseWriter

	status int
}

func (h *HTTPResponeRecorder) WriteHeader(statusCode int) {
	h.status = statusCode

	h.WriteHeader(statusCode)
}

func (h *HTTPTarget) Healthy() bool {
	return h.Healthchecker.IsHealthy()
}

func (h *HTTPTarget) Do(w http.ResponseWriter, r *http.Request) int {
	h.Proxy.ServeHTTP(w, r)

	return 200
}
