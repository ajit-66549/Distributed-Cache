package persistence

import "errors"

var (
	ErrInvalidHeader      = errors.New("invalid AOF header")
	ErrUnsupportedVersion = errors.New("unsupported persistence format version")
	ErrInvalidRecord      = errors.New("invalid persistence record")
	ErrRecordTooLarge     = errors.New("persistence record exceeds maximum payload size")
	ErrChecksumMismatch   = errors.New("persistence checksum mismatch")
	ErrTruncatedRecord    = errors.New("incomplete persistence record")
	ErrInvalidSequence    = errors.New("invalid persistence sequence")
)
