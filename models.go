package main

import (
	"encoding/json"
	"time"
)

// Tag identifies a named calibration/configuration set, e.g. "tracker-alignment".
type Tag struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

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

// CreateCalibrationRequest is the payload for POST /calibrations.
type CreateCalibrationRequest struct {
	Tag        string          `json:"tag" binding:"required"`
	ChannelID  int64           `json:"channel_id"`
	Since      int64           `json:"since" binding:"required"`
	Till       int64           `json:"till" binding:"required"`
	Data       json.RawMessage `json:"data" binding:"required"`
	InsertedBy string          `json:"inserted_by"`
	Comment    string          `json:"comment"`
}

// CorrectCalibrationRequest is the payload for PUT /calibrations/:tag/correct.
// It supersedes any active IOV(s) overlapping [Since, Till) for the given
// tag+channel with a new payload, bumping the revision.
type CorrectCalibrationRequest struct {
	Since      int64           `json:"since" binding:"required"`
	Till       int64           `json:"till" binding:"required"`
	Data       json.RawMessage `json:"data" binding:"required"`
	InsertedBy string          `json:"inserted_by"`
	Comment    string          `json:"comment"`
}

// CalibrationResponse bundles an IOV with its resolved payload data, as
// returned by the "valid at" lookup.
type CalibrationResponse struct {
	IOV     IOV             `json:"iov"`
	Payload json.RawMessage `json:"payload"`
}
