package cache

import "time"

type Entry struct {
	Value string
	ExpiresAt time.Time
}

func (e Entry) IsExpired(now time.Time) bool {
	if e.ExpiresAt.IsZero() {   // deadline is not assigned, means active, not expired
		return false
	}

	return !now.Before(e.ExpiresAt)  // current time is before deadline meaning value not expired
}
