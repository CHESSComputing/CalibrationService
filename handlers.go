package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	yaml "go.yaml.in/yaml/v3"
)

// Handler exposes the calibration IOV API over gin.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

// --- content negotiation aux functions ------------------------------------

const (
	contentTypeJSON = "application/json"
	contentTypeYAML = "application/x-yaml; charset=utf-8"
)

// isYAMLMediaType reports whether a Content-Type/Accept header value
// indicates YAML (application/x-yaml, application/yaml, text/yaml, or any
// value containing "yaml"/"yml").
func isYAMLMediaType(v string) bool {
	v = strings.ToLower(v)
	return strings.Contains(v, "yaml") || strings.Contains(v, "yml")
}

// isJSONMediaType reports whether a Content-Type/Accept header value
// indicates JSON.
func isJSONMediaType(v string) bool {
	return strings.Contains(strings.ToLower(v), "json")
}

// wantsYAML decides the response format for the current request, in order
// of precedence:
//  1. explicit "?format=yaml|yml|json" query parameter
//  2. the request's Accept header
//  3. mirroring the request's own Content-Type (so a YAML POST/PUT gets a
//     YAML response back without needing a separate Accept header)
//
// Defaults to JSON.
func wantsYAML(c *gin.Context) bool {
	if f := c.Query("format"); f != "" {
		return strings.EqualFold(f, "yaml") || strings.EqualFold(f, "yml")
	}
	if accept := c.GetHeader("Accept"); accept != "" && accept != "*/*" {
		if isYAMLMediaType(accept) {
			return true
		}
		if isJSONMediaType(accept) {
			return false
		}
	}
	return isYAMLMediaType(c.GetHeader("Content-Type"))
}

// bindBody reads and decodes the request body into out, honoring the
// request's Content-Type. YAML bodies (application/x-yaml, application/yaml,
// text/yaml) are decoded via YAML; anything else (including an empty
// Content-Type) is decoded as JSON. Both paths ultimately populate out via
// its "json" struct tags, so a single request struct (CalibrationRequest)
// works for either input format.
func bindBody(c *gin.Context, out interface{}) error {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return fmt.Errorf("%w: empty request body", ErrInvalidData)
	}

	if isYAMLMediaType(c.GetHeader("Content-Type")) {
		// Decode YAML into a generic value (yaml.v3 decodes mappings into
		// map[string]interface{}), then re-marshal to JSON so it can be
		// unmarshaled into "out" using the same "json" struct tags that
		// the JSON path uses. This keeps a single set of field names
		// ("label", "channel_id", "since", ...) regardless of input format.
		var generic interface{}
		if err := yaml.Unmarshal(body, &generic); err != nil {
			return fmt.Errorf("%w: invalid yaml body: %v", ErrInvalidData, err)
		}
		jsonBytes, err := json.Marshal(generic)
		if err != nil {
			return fmt.Errorf("yaml->json conversion: %w", err)
		}
		body = jsonBytes
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidData, err)
	}
	return nil
}

// render writes v to the response as JSON or YAML depending on wantsYAML.
// For YAML, v is round-tripped through JSON first (see bindBody's comment)
// so the emitted keys match the struct's "json" tags exactly.
func render(c *gin.Context, status int, v interface{}) {
	if !wantsYAML(c) {
		c.JSON(status, v)
		return
	}
	jsonBytes, err := json.Marshal(v)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("encode response: %v", err)})
		return
	}
	var generic interface{}
	if err := json.Unmarshal(jsonBytes, &generic); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("encode response: %v", err)})
		return
	}
	yamlBytes, err := yaml.Marshal(generic)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("encode response: %v", err)})
		return
	}
	c.Data(status, contentTypeYAML, yamlBytes)
}

// labelParam extracts and normalizes the hierarchical label from a gin
// wildcard route parameter (e.g. "*label"), which gin returns with its
// leading "/" intact - matching the "/3b/btr123/cycle123/sampleName" style.
func labelParam(c *gin.Context) (string, error) {
	label := c.Param("label")
	return normalizeLabel(label)
}

// --- handlers --------------------------------------------------------------

// POST /calibrations
// Body: CalibrationRequest (JSON or YAML, per Content-Type)
func (h *Handler) CreateCalibration(c *gin.Context) {
	var req CalibrationRequest
	if err := bindBody(c, &req); err != nil {
		writeErr(c, err)
		return
	}
	iov, err := h.store.InsertCalibration(c.Request.Context(), req)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusCreated, iov)
}

// GET /calibrations/valid/*label?at=<run_or_ts>&channel_id=<id>
func (h *Handler) GetValidCalibration(c *gin.Context) {
	label, err := labelParam(c)
	if err != nil {
		writeErr(c, err)
		return
	}

	at, err := strconv.ParseInt(c.Query("at"), 10, 64)
	if err != nil {
		writeErr(c, fmt.Errorf("%w: query param 'at' is required and must be an integer (run number or unix timestamp)", ErrInvalidData))
		return
	}
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		writeErr(c, fmt.Errorf("%w: invalid channel_id", ErrInvalidData))
		return
	}

	iov, data, err := h.store.GetValidCalibration(c.Request.Context(), label, channelID, at)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusOK, CalibrationResponse{IOV: *iov, Payload: data})
}

// GET /calibrations/label/*label?channel_id=<id>
func (h *Handler) ListCalibrations(c *gin.Context) {
	label, err := labelParam(c)
	if err != nil {
		writeErr(c, err)
		return
	}

	var channelID *int64
	if v := c.Query("channel_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(c, fmt.Errorf("%w: invalid channel_id", ErrInvalidData))
			return
		}
		channelID = &id
	}

	list, err := h.store.ListCalibrations(c.Request.Context(), label, channelID)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusOK, list)
}

// GET /calibrations/history/*label?channel_id=<id>
func (h *Handler) GetHistory(c *gin.Context) {
	label, err := labelParam(c)
	if err != nil {
		writeErr(c, err)
		return
	}
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		writeErr(c, fmt.Errorf("%w: invalid channel_id", ErrInvalidData))
		return
	}
	hist, err := h.store.GetHistory(c.Request.Context(), label, channelID)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusOK, hist)
}

// PUT /calibrations/correct/*label?channel_id=<id>
// Body: CalibrationRequest (JSON or YAML, per Content-Type)
func (h *Handler) CorrectCalibration(c *gin.Context) {
	label, err := labelParam(c)
	if err != nil {
		writeErr(c, err)
		return
	}
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		writeErr(c, fmt.Errorf("%w: invalid channel_id", ErrInvalidData))
		return
	}

	var req CalibrationRequest
	if err := bindBody(c, &req); err != nil {
		writeErr(c, err)
		return
	}

	iov, err := h.store.CorrectCalibration(c.Request.Context(), label, channelID, req)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusOK, iov)
}

// DELETE /calibrations/iov/:id
func (h *Handler) DeleteIOV(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		writeErr(c, fmt.Errorf("%w: invalid id", ErrInvalidData))
		return
	}
	if err := h.store.DeactivateIOV(c.Request.Context(), id); err != nil {
		writeErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DELETE /calibrations/label/*label?channel_id=<id>
// Deactivates every active IOV for the label (optionally scoped to one
// channel) in a single call, unlike DELETE /iov/:id which retracts one
// interval at a time.
func (h *Handler) DeleteByLabel(c *gin.Context) {
	label, err := labelParam(c)
	if err != nil {
		writeErr(c, err)
		return
	}

	var channelID *int64
	if v := c.Query("channel_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(c, fmt.Errorf("%w: invalid channel_id", ErrInvalidData))
			return
		}
		channelID = &id
	}

	n, err := h.store.DeactivateLabel(c.Request.Context(), label, channelID)
	if err != nil {
		writeErr(c, err)
		return
	}
	render(c, http.StatusOK, gin.H{
		"label":       label,
		"channel_id":  channelID,
		"deactivated": n,
	})
}

// writeErr maps store/validation sentinel errors onto HTTP status codes and
// renders them in the negotiated format (JSON or YAML).
func writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, sql.ErrNoRows):
		render(c, http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrOverlap):
		render(c, http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, ErrInvalidData):
		render(c, http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		render(c, http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
