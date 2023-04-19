package proxy

import (
	"net/http"
)

type ContextFailoverKeyInt int

const (
	Retries ContextFailoverKeyInt = iota
	TargetName
	VisitedTargets
)

// GetRetryFromContext returns the retries for request.
func GetRetryFromContext(r *http.Request) uint {
	if retries, ok := r.Context().Value(Retries).(uint); ok {
		return retries
	}
	return 0
}

// GetVisitedTargetsFromContext returns the visited targets for request.
func GetVisitedTargetsFromContext(r *http.Request) []uint {
	if visitedTargets, ok := r.Context().Value(VisitedTargets).([]uint); ok {
		return visitedTargets
	}
	return []uint{}
}

// GetTargetNameFromContext returns the target name for request.
func GetTargetNameFromContext(r *http.Request) string {
	if targetName, ok := r.Context().Value(TargetName).(string); ok {
		return targetName
	}
	return ""
}
