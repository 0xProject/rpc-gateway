package proxy

import (
	"net/http"
)

type ContextFailoverKeyInt int

const (
	TargetName ContextFailoverKeyInt = iota
	VisitedTargets
)

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
