package main

import (
	"net/http"
)

const (
	Attempts int = iota
	Retry
	TargetName
)

// GetAttemptsFromContext returns the attempts for request
func GetAttemptsFromContext(r *http.Request) uint {
	if attempts, ok := r.Context().Value(Attempts).(uint); ok {
		return attempts
	}
	return 1
}

// GetAttemptsFromContext returns the attempts for request
func GetRetryFromContext(r *http.Request) uint {
	if retry, ok := r.Context().Value(Retry).(uint); ok {
		return retry
	}
	return 0
}

// GetTargetNameFromContext returns the attempts for request
func GetTargetNameFromContext(r *http.Request) string {
	if retry, ok := r.Context().Value(TargetName).(string); ok {
		return retry
	}
	return ""
}

func ReadUserIP(r *http.Request) string {
	IPAddress := r.Header.Get("X-Real-Ip")
	if IPAddress == "" {
		IPAddress = r.Header.Get("X-Forwarded-For")
	}
	if IPAddress == "" {
		IPAddress = r.RemoteAddr
	}
	return IPAddress
}
