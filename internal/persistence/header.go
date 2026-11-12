// Package persistence implements cache log storage and its on-disk format.
// The codecs serialize data; AOF manages durable file appends.
package persistence

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
)

const (
	FormatVersion uint16 = 1
	HeaderSize           = 30

	aofMagic = "CACHEAOF"
)

// Header describes the format and history represented by an AOF.
type Header struct {
	Version      uint16
	BaseSequence uint64
	MaxBytes     int64
}

// EncodeHeader returns the fixed-size, checksummed version 1 file header.
func EncodeHeader(header Header) ([]byte, error) {
	if header.Version != FormatVersion {
		return nil, fmt.Errorf("%w: %w: %d", ErrInvalidHeader, ErrUnsupportedVersion, header.Version)
	}

	if header.MaxBytes <= 0 {
		return nil, fmt.Errorf("%w: memory budget must be positive", ErrInvalidHeader)
	}

	data := make([]byte, HeaderSize)
	copy(data[:8], aofMagic)
	binary.BigEndian.PutUint16(data[8:10], header.Version)
	binary.BigEndian.PutUint64(data[10:18], header.BaseSequence)
	binary.BigEndian.PutUint64(data[18:26], uint64(header.MaxBytes))
	binary.BigEndian.PutUint32(data[26:30], crc32.ChecksumIEEE(data[:26]))

	return data, nil
}

// DecodeHeader consumes exactly one file header. Even an empty input is an
// invalid header; only DecodeRecord uses io.EOF to indicate a clean log end.
func DecodeHeader(reader io.Reader) (Header, error) {
	var data [HeaderSize]byte
	if _, err := io.ReadFull(reader, data[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Header{}, fmt.Errorf("%w: incomplete file header: %v", ErrInvalidHeader, err)
		}
		return Header{}, fmt.Errorf("read AOF header: %w", err)
	}

	if binary.BigEndian.Uint32(data[26:30]) != crc32.ChecksumIEEE(data[:26]) {
		return Header{}, fmt.Errorf("%w: %w", ErrInvalidHeader, ErrChecksumMismatch)
	}
	if string(data[:8]) != aofMagic {
		return Header{}, fmt.Errorf("%w: unexpected magic", ErrInvalidHeader)
	}
	version := binary.BigEndian.Uint16(data[8:10])
	if version != FormatVersion {
		return Header{}, fmt.Errorf("%w: %w: %d", ErrInvalidHeader, ErrUnsupportedVersion, version)
	}
	maxBytes := binary.BigEndian.Uint64(data[18:26])
	if maxBytes == 0 || maxBytes > math.MaxInt64 {
		return Header{}, fmt.Errorf("%w: memory budget must be a positive signed 64-bit integer", ErrInvalidHeader)
	}

	return Header{
		Version:      version,
		BaseSequence: binary.BigEndian.Uint64(data[10:18]),
		MaxBytes:     int64(maxBytes),
	}, nil
}
