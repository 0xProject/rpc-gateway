package proxy

import (
	"bytes"
	"net/http"
)

type ReponseWriter struct {
	body       *bytes.Buffer
	header     http.Header
	statusCode int
}

func (p *ReponseWriter) Header() http.Header {
	return p.header
}

func (p *ReponseWriter) Write(b []byte) (int, error) {
	return p.body.Write(b)
}

func (p *ReponseWriter) WriteHeader(statusCode int) {
	p.statusCode = statusCode
}

func NewResponseWriter() *ReponseWriter {
	return &ReponseWriter{
		header: http.Header{},
		body:   &bytes.Buffer{},
	}
}
