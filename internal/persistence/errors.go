package persistence

import "errors"

var (
	ErrInvalidHeader        = errors.New("invalid AOF header")
	ErrUnsupportedVersion   = errors.New("unsupported persistence format version")
	ErrInvalidRecord        = errors.New("invalid persistence record")
	ErrRecordTooLarge       = errors.New("persistence record exceeds maximum payload size")
	ErrChecksumMismatch     = errors.New("persistence checksum mismatch")
	ErrTruncatedRecord      = errors.New("incomplete persistence record")
	ErrInvalidSequence      = errors.New("invalid persistence sequence")
	ErrAOFClosed            = errors.New("append-only log is closed")
	ErrAOFFailed            = errors.New("append-only log requires recovery after an I/O failure")
	ErrAOFInUse             = errors.New("cache data directory is already in use")
	ErrRecoveryRequired     = errors.New("cache data requires recovery before append")
	ErrMemoryBudgetMismatch = errors.New("persisted cache memory budget does not match configuration")
	ErrUnsupportedPlatform  = errors.New("persistence requires macOS or Linux")
)
