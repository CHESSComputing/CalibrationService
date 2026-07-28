package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Handler exposes the calibration IOV API over gin.
type Handler struct {
	store *Store
}

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

/*
// RegisterRoutes wires the handler's endpoints onto a gin engine/group.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/calibrations")
	{
		api.POST("", h.CreateCalibration)              // add a new calibration + IOV
		api.GET("/:tag", h.ListCalibrations)            // list active IOVs for a tag
		api.GET("/:tag/valid", h.GetValidCalibration)   // resolve constants valid "at" a run/time
		api.GET("/:tag/history", h.GetHistory)          // full revision history for tag+channel
		api.PUT("/:tag/correct", h.CorrectCalibration)  // supersede overlapping IOV(s)
		api.DELETE("/iov/:id", h.DeleteIOV)             // retract a single IOV
	}
}
*/

// POST /calibrations
// Body: CreateCalibrationRequest
func (h *Handler) CreateCalibration(c *gin.Context) {
	var req CreateCalibrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	iov, err := h.store.InsertCalibration(c.Request.Context(), req)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, iov)
}

// GET /calibrations/:tag/valid?at=<run_or_ts>&channel_id=<id>
func (h *Handler) GetValidCalibration(c *gin.Context) {
	tag := c.Param("tag")

	at, err := strconv.ParseInt(c.Query("at"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "query param 'at' is required and must be an integer (run number or unix timestamp)",
		})
		return
	}
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel_id"})
		return
	}

	iov, data, err := h.store.GetValidCalibration(c.Request.Context(), tag, channelID, at)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, CalibrationResponse{IOV: *iov, Payload: data})
}

// GET /calibrations/:tag?channel_id=<id>
func (h *Handler) ListCalibrations(c *gin.Context) {
	tag := c.Param("tag")

	var channelID *int64
	if v := c.Query("channel_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel_id"})
			return
		}
		channelID = &id
	}

	list, err := h.store.ListCalibrations(c.Request.Context(), tag, channelID)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, list)
}

// GET /calibrations/:tag/history?channel_id=<id>
func (h *Handler) GetHistory(c *gin.Context) {
	tag := c.Param("tag")
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel_id"})
		return
	}
	hist, err := h.store.GetHistory(c.Request.Context(), tag, channelID)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, hist)
}

// PUT /calibrations/:tag/correct?channel_id=<id>
// Body: CorrectCalibrationRequest
func (h *Handler) CorrectCalibration(c *gin.Context) {
	tag := c.Param("tag")
	channelID, err := strconv.ParseInt(c.DefaultQuery("channel_id", "0"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel_id"})
		return
	}

	var req CorrectCalibrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	iov, err := h.store.CorrectCalibration(c.Request.Context(), tag, channelID, req)
	if err != nil {
		writeStoreErr(c, err)
		return
	}
	c.JSON(http.StatusOK, iov)
}

// DELETE /calibrations/iov/:id
func (h *Handler) DeleteIOV(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	if err := h.store.DeactivateIOV(c.Request.Context(), id); err != nil {
		writeStoreErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// writeStoreErr maps store-layer sentinel errors onto HTTP status codes.
func writeStoreErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, ErrOverlap):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
