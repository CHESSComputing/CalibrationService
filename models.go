package main

import (
	"encoding/json"
	"time"
)

// CalibrationRequest is the payload for creating a new calibration (POST)
// and for corrections (PUT). Label uses a hierarchical path, e.g.
// "/3b/btr123/cycle123/sampleName". There is no "till": the row is valid
// from Since onward until a later Since (for the same label+channel)
// supersedes it - the most recently started entry is always the default.
type CalibrationRequest struct {
	Label      string          `json:"label"`
	ChannelID  int64           `json:"channel_id"`
	Since      int64           `json:"since"`
	Data       json.RawMessage `json:"data"`
	InsertedBy string          `json:"inserted_by,omitempty"`
	Comment    string          `json:"comment,omitempty"`
}

// CalibrationIOV binds a payload to a label+channel starting at Since, with
// no explicit end - it remains in effect until a later Since supersedes it.
type CalibrationIOV struct {
	ID         int64     `json:"id"`
	LabelID    int64     `json:"label_id"`
	Label      string    `json:"label,omitempty"`
	ChannelID  int64     `json:"channel_id"`
	PayloadID  int64     `json:"payload_id"`
	Since      int64     `json:"since"`
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
