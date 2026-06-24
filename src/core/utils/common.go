package utils

import (
	"os"
	"time"
)

// GetProjectDir gets the project root directory
func GetProjectDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

// MinDuration is a helper that returns the smaller of two time durations
func MinDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
