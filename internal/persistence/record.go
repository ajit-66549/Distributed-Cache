package persistence

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
)

const (
	MaxRecordPayloadSize = 64 * 1024 * 1024

	recordHeaderSize = 8
	maxKeyBytes      = 256
	maxValueBytes    = 1 << 20
)

type Operation string

const (
	OpSet    Operation = "SET"
	OpDelete Operation = "DELETE"
)

// Mutation is one operation inside a record. For SET, a nil deadline means
// no expiration. DELETE must have an empty Value and a nil deadline.
type Mutation struct {
	Op                Operation
	Key               string
	Value             string
	ExpiresAtUnixNano *int64
}

// Record represents one client mutation, including any evictions caused by SET.
// It contains either one DELETE or zero or more eviction DELETEs followed by SET.
type Record struct {
	Sequence  uint64
	Mutations []Mutation
}

// Validate checks one record independently of the surrounding log.
func (record Record) Validate() error {
	if record.Sequence == 0 {
		return fmt.Errorf("%w: %w: sequence must be positive", ErrInvalidRecord, ErrInvalidSequence)
	}
	if len(record.Mutations) == 0 {
		return fmt.Errorf("%w: mutations must not be empty", ErrInvalidRecord)
	}
	for index, mutation := range record.Mutations {
		if err := mutation.validate(); err != nil {
			return fmt.Errorf("%w: mutation %d: %w", ErrInvalidRecord, index, err)
		}
		if mutation.Op == OpSet && index != len(record.Mutations)-1 {
			return fmt.Errorf("%w: SET must be the final mutation", ErrInvalidRecord)
		}
	}
	if len(record.Mutations) > 1 && record.Mutations[len(record.Mutations)-1].Op != OpSet {
		return fmt.Errorf("%w: multiple DELETE mutations require a final SET", ErrInvalidRecord)
	}
	return nil
}

// ValidateAfter checks sequence continuity against a file's base sequence or
// the previously decoded record. Log writers and recovery must call it in order.
func (record Record) ValidateAfter(previousSequence uint64) error {
	if previousSequence == math.MaxUint64 {
		return fmt.Errorf("%w: sequence exhausted", ErrInvalidSequence)
	}
	if record.Sequence != previousSequence+1 {
		return fmt.Errorf("%w: got %d, want %d", ErrInvalidSequence, record.Sequence, previousSequence+1)
	}
	return nil
}

func (mutation Mutation) validate() error {
	if len(mutation.Key) == 0 || len(mutation.Key) > maxKeyBytes || !utf8.ValidString(mutation.Key) {
		return fmt.Errorf("key must contain 1 to %d valid UTF-8 bytes", maxKeyBytes)
	}
	switch mutation.Op {
	case OpSet:
		if len(mutation.Value) > maxValueBytes || !utf8.ValidString(mutation.Value) {
			return fmt.Errorf("value must contain at most %d valid UTF-8 bytes", maxValueBytes)
		}
	case OpDelete:
		if mutation.Value != "" || mutation.ExpiresAtUnixNano != nil {
			return fmt.Errorf("DELETE cannot carry a value or expiration")
		}
	default:
		return fmt.Errorf("unsupported operation %q", mutation.Op)
	}
	return nil
}

// EncodeRecord returns one complete frame, including its length and checksums.
// It builds the payload one mutation at a time to enforce the size limit without
// first marshaling an arbitrarily large batch into a temporary buffer.
func EncodeRecord(record Record) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}

	frame := make([]byte, recordHeaderSize)
	frame = append(frame, `{"sequence":`...)
	frame = strconv.AppendUint(frame, record.Sequence, 10)
	frame = append(frame, `,"mutations":[`...)
	for index, mutation := range record.Mutations {
		wire := struct {
			Op                Operation       `json:"op"`
			Key               string          `json:"key"`
			Value             *string         `json:"value,omitempty"`
			ExpiresAtUnixNano json.RawMessage `json:"expires_at_unix_nano,omitempty"`
		}{Op: mutation.Op, Key: mutation.Key}
		if mutation.Op == OpSet {
			wire.Value = &mutation.Value
			wire.ExpiresAtUnixNano = json.RawMessage("null")
			if mutation.ExpiresAtUnixNano != nil {
				wire.ExpiresAtUnixNano = strconv.AppendInt(nil, *mutation.ExpiresAtUnixNano, 10)
			}
		}
		encoded, err := json.Marshal(wire)
		if err != nil {
			return nil, fmt.Errorf("%w: encode mutation %d: %w", ErrInvalidRecord, index, err)
		}
		additional := len(encoded) + 2 // Reserve the closing array and object.
		if index > 0 {
			additional++ // Comma before this mutation.
		}
		if len(frame)-recordHeaderSize > MaxRecordPayloadSize-additional {
			return nil, ErrRecordTooLarge
		}
		if index > 0 {
			frame = append(frame, ',')
		}
		frame = append(frame, encoded...)
	}
	frame = append(frame, ']', '}')

	payload := frame[recordHeaderSize:]
	binary.BigEndian.PutUint32(frame[:4], uint32(len(payload)))
	binary.BigEndian.PutUint32(frame[4:8], crc32.ChecksumIEEE(frame[:4]))
	checksum := crc32.Update(crc32.ChecksumIEEE(frame[:4]), crc32.IEEETable, payload)
	return binary.BigEndian.AppendUint32(frame, checksum), nil
}

// DecodeRecord consumes one frame and returns a record only after full
// validation. io.EOF means no bytes of a new frame were read; an incomplete
// frame returns ErrTruncatedRecord instead. Sequence continuity is checked
// separately with ValidateAfter because this function has no log state.
func DecodeRecord(reader io.Reader) (Record, error) {
	var header [recordHeaderSize]byte
	count, err := io.ReadFull(reader, header[:])
	if err != nil {
		if count == 0 && err == io.EOF {
			return Record{}, io.EOF
		}
		return Record{}, recordReadError("frame header", err)
	}
	if binary.BigEndian.Uint32(header[4:8]) != crc32.ChecksumIEEE(header[:4]) {
		return Record{}, fmt.Errorf("%w: length header", ErrChecksumMismatch)
	}
	payloadLength := binary.BigEndian.Uint32(header[:4])
	if payloadLength == 0 {
		return Record{}, fmt.Errorf("%w: payload must not be empty", ErrInvalidRecord)
	}
	if payloadLength > MaxRecordPayloadSize {
		return Record{}, ErrRecordTooLarge
	}

	payload := make([]byte, int(payloadLength))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Record{}, recordReadError("payload", err)
	}
	var trailer [4]byte
	if _, err := io.ReadFull(reader, trailer[:]); err != nil {
		return Record{}, recordReadError("record checksum", err)
	}
	checksum := crc32.Update(crc32.ChecksumIEEE(header[:4]), crc32.IEEETable, payload)
	if binary.BigEndian.Uint32(trailer[:]) != checksum {
		return Record{}, fmt.Errorf("%w: record payload", ErrChecksumMismatch)
	}
	if !utf8.Valid(payload) {
		return Record{}, fmt.Errorf("%w: payload is not valid UTF-8", ErrInvalidRecord)
	}

	fields, err := decodeObject(payload, "sequence", "mutations")
	if err != nil {
		return Record{}, fmt.Errorf("%w: %v", ErrInvalidRecord, err)
	}
	var record Record
	if err := json.Unmarshal(fields["sequence"], &record.Sequence); err != nil {
		return Record{}, fmt.Errorf("%w: sequence: %w", ErrInvalidRecord, err)
	}
	record.Mutations, err = decodeMutations(fields["mutations"])
	if err != nil {
		return Record{}, fmt.Errorf("%w: mutations: %v", ErrInvalidRecord, err)
	}
	if err := record.Validate(); err != nil {
		return Record{}, err
	}
	return record, nil
}

func recordReadError(part string, err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		// Do not wrap io.EOF: errors.Is(err, io.EOF) must not mistake a
		// damaged tail for the clean end of a log.
		return fmt.Errorf("%w: %s: %v", ErrTruncatedRecord, part, err)
	}
	return fmt.Errorf("read record %s: %w", part, err)
}

func decodeMutations(data []byte) ([]Mutation, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('[') {
		return nil, fmt.Errorf("expected an array")
	}
	var mutations []Mutation
	for decoder.More() {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		mutation, err := decodeMutation(raw)
		if err != nil {
			return nil, fmt.Errorf("mutation %d: %w", len(mutations), err)
		}
		if len(mutations) > 0 && mutations[len(mutations)-1].Op == OpSet {
			return nil, fmt.Errorf("SET must be the final mutation")
		}
		mutations = append(mutations, mutation)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	return mutations, requireJSONEnd(decoder)
}

func decodeMutation(data []byte) (Mutation, error) {
	fields, err := decodeObject(data, "op", "key", "value", "expires_at_unix_nano")
	if err != nil {
		return Mutation{}, err
	}
	var mutation Mutation
	if err := json.Unmarshal(fields["op"], &mutation.Op); err != nil {
		return Mutation{}, fmt.Errorf("op: %w", err)
	}
	if err := json.Unmarshal(fields["key"], &mutation.Key); err != nil {
		return Mutation{}, fmt.Errorf("key: %w", err)
	}
	switch mutation.Op {
	case OpSet:
		value, ok := fields["value"]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Mutation{}, fmt.Errorf("SET requires a string value")
		}
		if err := json.Unmarshal(value, &mutation.Value); err != nil {
			return Mutation{}, fmt.Errorf("value: %w", err)
		}
		expiration, ok := fields["expires_at_unix_nano"]
		if !ok {
			return Mutation{}, fmt.Errorf("SET requires expires_at_unix_nano, using null for no expiration")
		}
		if err := json.Unmarshal(expiration, &mutation.ExpiresAtUnixNano); err != nil {
			return Mutation{}, fmt.Errorf("expires_at_unix_nano: %w", err)
		}
	case OpDelete:
		if _, ok := fields["value"]; ok {
			return Mutation{}, fmt.Errorf("DELETE cannot carry a value")
		}
		if _, ok := fields["expires_at_unix_nano"]; ok {
			return Mutation{}, fmt.Errorf("DELETE cannot carry an expiration")
		}
	}
	if err := mutation.validate(); err != nil {
		return Mutation{}, err
	}
	return mutation, nil
}

// decodeObject enforces exact field names and rejects duplicate fields. The
// standard JSON struct decoder accepts duplicates and case-insensitive names.
func decodeObject(data []byte, allowedFields ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if token != json.Delim('{') {
		return nil, fmt.Errorf("expected an object")
	}
	fields := make(map[string]json.RawMessage, len(allowedFields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected a field name")
		}
		allowed := false
		for _, field := range allowedFields {
			if name == field {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("duplicate field %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := requireJSONEnd(decoder); err != nil {
		return nil, err
	}
	return fields, nil
}

func requireJSONEnd(decoder *json.Decoder) error {
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return fmt.Errorf("trailing JSON data: %w", err)
		}
		return fmt.Errorf("unexpected trailing JSON value")
	}
	return nil
}
