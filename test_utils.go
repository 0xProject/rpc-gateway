package main

import (
	"net/http"
)

type responder struct {
	value     []byte
	onRequest func(*http.Request)
}

func (r *responder) SetValue(value []byte) {
	r.value = value
}

func (r *responder) OnRequest(function func(*http.Request)) {
	r.onRequest = function
}

func (r *responder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.onRequest(req)
	w.Write(r.value)
}
