package rpcgateway

import (
	"net/http"
)

type responder struct {
	value      []byte
	onRequest  func(*http.Request)
	onResponse func(http.ResponseWriter)
}

func (r *responder) SetValue(value []byte) {
	r.value = value
}

func (r *responder) OnRequest(function func(*http.Request)) {
	r.onRequest = function
}

func (r *responder) OnResponse(function func(http.ResponseWriter)) {
	r.onResponse = function
}

func (r *responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if r.onRequest != nil {
		r.onRequest(req)
	}

	if r.onResponse != nil {
		r.onResponse(w)
	}

	w.Write(r.value)
}
