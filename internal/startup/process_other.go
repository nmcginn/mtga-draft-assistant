//go:build !linux && !darwin

package startup

import (
	"errors"
	"time"
)

// processStart is not implemented where the app is not yet shipped
// (decision 3); the report counts from main instead.
func processStart(time.Time) (time.Time, error) {
	return time.Time{}, errors.New("process start time is not available on this platform")
}
