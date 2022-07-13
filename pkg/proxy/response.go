package proxy

import "net/http"

type HTTPStatusRecorder struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func NewHTTPStatusRecorder(w http.ResponseWriter) *HTTPStatusRecorder {
	return &HTTPStatusRecorder{ResponseWriter: w}
}

func (r *HTTPStatusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}

	r.status = status
	r.ResponseWriter.WriteHeader(status)
	r.wroteHeader = true
}
