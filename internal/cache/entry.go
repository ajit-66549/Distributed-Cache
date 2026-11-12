package cache

import (
	"errors"
	"math"
	"time"
)

var ErrExpirationOutOfRange = errors.New("expiration is outside the Unix nanosecond range")

type Entry struct {
	Value     string
	ExpiresAt time.Time
}

// expirationUnixNano converts a deadline to the log format without changing it.
// A nil result represents an entry without expiration.
func (e Entry) expirationUnixNano() (*int64, error) {
	if e.ExpiresAt.IsZero() {
		return nil, nil
	}
	if e.ExpiresAt.Before(time.Unix(0, math.MinInt64)) || e.ExpiresAt.After(time.Unix(0, math.MaxInt64)) {
		return nil, ErrExpirationOutOfRange
	}
	expiration := e.ExpiresAt.UnixNano()
	return &expiration, nil
}

func (e Entry) IsExpired(now time.Time) bool {
	if e.ExpiresAt.IsZero() { // deadline is not assigned, means active, not expired
		return false
	}

	return !now.Before(e.ExpiresAt) // current time is before deadline meaning value not expired
}
