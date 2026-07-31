package main

import (
	"encoding/json"
	"time"
)

// Payload is an immutable, versioned constants blob for a tag.
type Payload struct {
	ID        int64           `json:"id"`
	TagID     int64           `json:"tag_id"`
	Data      json.RawMessage `json:"data"`
	Checksum  string          `json:"checksum"`
	CreatedAt time.Time       `json:"created_at"`
}

// IOV (interval of validity) binds a payload to a tag+channel for a
// half-open range [Since, Till).
type IOV struct {
	ID         int64     `json:"id"`
	TagID      int64     `json:"tag_id"`
	TagName    string    `json:"tag_name,omitempty"`
	ChannelID  int64     `json:"channel_id"`
	PayloadID  int64     `json:"payload_id"`
	Since      int64     `json:"since"`
	Till       int64     `json:"till"`
	Revision   int       `json:"revision"`
	IsActive   bool      `json:"is_active"`
	InsertedAt time.Time `json:"inserted_at"`
	InsertedBy string    `json:"inserted_by,omitempty"`
	Comment    string    `json:"comment,omitempty"`
}

// CalibrationRequest is the payload for creating a new calibration (POST)
// and for corrections (PUT). Label uses a hierarchical path, e.g.
// "/3b/btr123/cycle123/sampleName".
type CalibrationRequest struct {
	Label      string          `json:"label"`
	ChannelID  int64           `json:"channel_id"`
	Since      int64           `json:"since"`
	Till       int64           `json:"till"`
	Data       json.RawMessage `json:"data"`
	InsertedBy string          `json:"inserted_by,omitempty"`
	Comment    string          `json:"comment,omitempty"`
}

// CalibrationIOV (interval of validity) binds a payload to a label+channel
// for a half-open range [Since, Till).
type CalibrationIOV struct {
	ID         int64     `json:"id"`
	LabelID    int64     `json:"label_id"`
	Label      string    `json:"label,omitempty"`
	ChannelID  int64     `json:"channel_id"`
	PayloadID  int64     `json:"payload_id"`
	Since      int64     `json:"since"`
	Till       int64     `json:"till"`
	Revision   int       `json:"revision"`
	IsActive   bool      `json:"is_active"`
	InsertedAt time.Time `json:"inserted_at"`
	InsertedBy string    `json:"inserted_by,omitempty"`
	Comment    string    `json:"comment,omitempty"`
}

// CalibrationResponse bundles an IOV with its resolved payload data, as
// returned by the "valid at" lookup.
type CalibrationResponse struct {
	IOV     CalibrationIOV  `json:"iov"`
	Payload json.RawMessage `json:"payload"`
}

