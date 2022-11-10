package util

import "os"

// This is workaround until we will clean up our test practices.
func Getenv(key string, fallback string) string {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}

	return value
}
