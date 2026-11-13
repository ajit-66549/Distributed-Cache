package server

import (
	"errors"
	"math"
	"time"
)

const maxKeyLength = 256
const maxValueSize = 1 << 20 // 1 MiB
const maxTTLSeconds = math.MaxInt64 / int64(time.Second)

var (
	errKeyRequired   = errors.New("Key is required")
	errKeyTooLong    = errors.New("key exceeds maximum length")
	errValueTooLarge = errors.New("value exceeds maximum size")
	errTTLInvalid    = errors.New("ttl must be greater than zero")
	errTTLTooLarge   = errors.New("ttl exceeds maximum supported duration")
)

func validateKey(key string) error {
	if key == "" {
		return errKeyRequired
	}

	if len(key) > maxKeyLength {
		return errKeyTooLong
	}

	return nil
}

func valueValidate(value string) error {
	if len(value) > maxValueSize {
		return errValueTooLarge
	}
	return nil
}

func validateTTL(ttlSeconds *int64) error {
	if ttlSeconds == nil {
		return nil
	}
	if *ttlSeconds <= 0 {
		return errTTLInvalid
	}
	if *ttlSeconds > maxTTLSeconds {
		return errTTLTooLarge
	}
	return nil
}
