package http

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/nurpe/snowops-contract/internal/http/middleware"
	"github.com/nurpe/snowops-contract/internal/model"
	"github.com/nurpe/snowops-contract/internal/service"
)

type Handler struct {
	contracts *service.ContractService
	log       zerolog.Logger
}

func NewHandler(
	contracts *service.ContractService,
	log zerolog.Logger,
) *Handler {
	return &Handler{
		contracts: contracts,
		log:       log,
	}
}

func (h *Handler) Register(r *gin.Engine, authMiddleware gin.HandlerFunc) {
	protected := r.Group("/")
	protected.Use(authMiddleware)

	protected.GET("/contracts", h.listContracts)
	protected.POST("/contracts", h.createContract)
	protected.GET("/contracts/:id", h.getContract)
	protected.GET("/contracts/:id/deletion-info", h.getContractDeletionInfo)
	protected.DELETE("/contracts/:id", h.deleteContract)
	protected.GET("/contracts/:id/tickets", h.listContractTickets)
	protected.GET("/contracts/:id/trips", h.listContractTrips)
	protected.PUT("/tickets/:ticket_id/contract", h.assignTicketContract)
	protected.POST("/trips/usage", h.recordTripUsage)
}

func (h *Handler) listContracts(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	var contractorID *uuid.UUID
	if raw := c.Query("contractor_id"); raw != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("invalid contractor_id"))
			return
		}
		contractorID = &parsed
	}

	var landfillID *uuid.UUID
	if raw := c.Query("landfill_id"); raw != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("invalid landfill_id"))
			return
		}
		landfillID = &parsed
	}

	var contractType *model.ContractType
	if raw := c.Query("contract_type"); raw != "" {
		value := model.ContractType(strings.ToUpper(strings.TrimSpace(raw)))
		if value != model.ContractTypeContractorService && value != model.ContractTypeLandfillService {
			c.JSON(http.StatusBadRequest, errorResponse("invalid contract_type"))
			return
		}
		contractType = &value
	}

	var workType *model.WorkType
	if raw := c.Query("work_type"); raw != "" {
		value := model.WorkType(strings.ToLower(strings.TrimSpace(raw)))
		if value != model.WorkTypeRoad &&
			value != model.WorkTypeSidewalk &&
			value != model.WorkTypeYard {
			c.JSON(http.StatusBadRequest, errorResponse("invalid work_type"))
			return
		}
		workType = &value
	}

	var status *model.ContractUIStatus
	if raw := c.Query("status"); raw != "" {
		value := model.ContractUIStatus(strings.ToUpper(strings.TrimSpace(raw)))
		if value != model.ContractUIStatusPlanned &&
			value != model.ContractUIStatusActive &&
			value != model.ContractUIStatusExpired &&
			value != model.ContractUIStatusArchived {
			c.JSON(http.StatusBadRequest, errorResponse("invalid status"))
			return
		}
		status = &value
	}

	parseTimeQuery := func(key string) (*time.Time, error) {
		if raw := c.Query(key); raw != "" {
			t, err := parseTime(raw)
			if err != nil {
				return nil, err
			}
			return &t, nil
		}
		return nil, nil
	}

	startFrom, err := parseTimeQuery("start_from")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid start_from"))
		return
	}
	startTo, err := parseTimeQuery("start_to")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid start_to"))
		return
	}
	endFrom, err := parseTimeQuery("end_from")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid end_from"))
		return
	}
	endTo, err := parseTimeQuery("end_to")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid end_to"))
		return
	}

	onlyActive := parseBoolQuery(c.Query("only_active"))

	contracts, err := h.contracts.List(
		c.Request.Context(),
		principal,
		service.ListContractsInput{
			ContractorID: contractorID,
			LandfillID:   landfillID,
			ContractType: contractType,
			WorkType:     workType,
			OnlyActive:   onlyActive,
			Status:       status,
			StartFrom:    startFrom,
			StartTo:      startTo,
			EndFrom:      endFrom,
			EndTo:        endTo,
		},
	)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(contracts))
}

type createContractRequest struct {
	ContractType    string      `json:"contract_type" binding:"required"`
	ContractorID    *string     `json:"contractor_id"` // Опционально для LANDFILL_SERVICE
	LandfillID      *string     `json:"landfill_id"`   // Опционально для CONTRACTOR_SERVICE
	PolygonIDs      []uuid.UUID `json:"polygon_ids"`   // Обязательно для LANDFILL_SERVICE
	Name            string      `json:"name" binding:"required"`
	WorkType        *string     `json:"work_type"` // Опционально для LANDFILL_SERVICE
	PricePerM3      float64     `json:"price_per_m3" binding:"required,gt=0"`
	BudgetTotal     float64     `json:"budget_total" binding:"required,gt=0"`
	MinimalVolumeM3 float64     `json:"minimal_volume_m3" binding:"required,gt=0"`
	StartAt         string      `json:"start_at" binding:"required"`
	EndAt           string      `json:"end_at" binding:"required"`
	IsActive        *bool       `json:"is_active"`
}

func (h *Handler) createContract(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	var req createContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
		return
	}

	contractType := model.ContractType(strings.ToUpper(strings.TrimSpace(req.ContractType)))
	if contractType != model.ContractTypeContractorService && contractType != model.ContractTypeLandfillService {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract_type"))
		return
	}

	var contractorID *uuid.UUID
	if req.ContractorID != nil {
		parsed, err := uuid.Parse(strings.TrimSpace(*req.ContractorID))
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("invalid contractor_id"))
			return
		}
		contractorID = &parsed
	}

	var landfillID *uuid.UUID
	if req.LandfillID != nil {
		parsed, err := uuid.Parse(strings.TrimSpace(*req.LandfillID))
		if err != nil {
			c.JSON(http.StatusBadRequest, errorResponse("invalid landfill_id"))
			return
		}
		landfillID = &parsed
	}

	var workType model.WorkType
	if req.WorkType != nil {
		wt := model.WorkType(strings.ToLower(strings.TrimSpace(*req.WorkType)))
		if wt != model.WorkTypeRoad && wt != model.WorkTypeSidewalk && wt != model.WorkTypeYard {
			c.JSON(http.StatusBadRequest, errorResponse("invalid work_type"))
			return
		}
		workType = wt
	}

	startAt, err := parseTime(req.StartAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid start_at format"))
		return
	}

	endAt, err := parseTime(req.EndAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid end_at format"))
		return
	}

	contract, err := h.contracts.Create(
		c.Request.Context(),
		principal,
		service.CreateContractInput{
			ContractType:    contractType,
			ContractorID:    contractorID,
			LandfillID:      landfillID,
			PolygonIDs:      req.PolygonIDs,
			Name:            req.Name,
			WorkType:        workType,
			PricePerM3:      req.PricePerM3,
			BudgetTotal:     req.BudgetTotal,
			MinimalVolumeM3: req.MinimalVolumeM3,
			StartAt:         startAt,
			EndAt:           endAt,
			IsActive:        req.IsActive,
		},
	)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, successResponse(contract))
}

func (h *Handler) getContract(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	contractID, err := parseUUIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract id"))
		return
	}

	contract, err := h.contracts.Get(c.Request.Context(), principal, contractID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(contract))
}

func (h *Handler) listContractTickets(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	contractID, err := parseUUIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract id"))
		return
	}

	items, err := h.contracts.ListContractTickets(c.Request.Context(), principal, contractID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(items))
}

func (h *Handler) listContractTrips(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	contractID, err := parseUUIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract id"))
		return
	}

	items, err := h.contracts.ListContractTrips(c.Request.Context(), principal, contractID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(items))
}

type assignTicketContractRequest struct {
	ContractID string `json:"contract_id" binding:"required"`
}

func (h *Handler) assignTicketContract(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	ticketID, err := parseUUIDParam(c, "ticket_id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid ticket id"))
		return
	}

	var req assignTicketContractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
		return
	}

	contractID, err := uuid.Parse(strings.TrimSpace(req.ContractID))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract_id"))
		return
	}

	err = h.contracts.AssignTicketContract(c.Request.Context(), principal, service.AssignTicketContractInput{
		TicketID:   ticketID,
		ContractID: contractID,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(gin.H{"status": "linked"}))
}

type recordTripUsageRequest struct {
	TripID           string  `json:"trip_id" binding:"required"`
	TicketID         string  `json:"ticket_id" binding:"required"`
	DetectedVolumeM3 float64 `json:"detected_volume_m3" binding:"required,gt=0"`
}

func (h *Handler) recordTripUsage(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	var req recordTripUsageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
		return
	}

	tripID, err := uuid.Parse(strings.TrimSpace(req.TripID))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid trip_id"))
		return
	}
	ticketID, err := uuid.Parse(strings.TrimSpace(req.TicketID))
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid ticket_id"))
		return
	}

	err = h.contracts.RecordTripUsage(c.Request.Context(), principal, service.RecordTripUsageInput{
		TripID:   tripID,
		TicketID: ticketID,
		VolumeM3: req.DetectedVolumeM3,
	})
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusCreated, successResponse(gin.H{"status": "recorded"}))
}

func (h *Handler) getContractDeletionInfo(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	contractID, err := parseUUIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract id"))
		return
	}

	info, err := h.contracts.GetDeletionInfo(c.Request.Context(), principal, contractID)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, successResponse(gin.H{
		"contract": gin.H{
			"id":   info.Contract.ID,
			"name": info.Contract.Name,
		},
		"dependencies": gin.H{
			"tickets_count":     info.Dependencies.TicketsCount,
			"trips_count":       info.Dependencies.TripsCount,
			"assignments_count": info.Dependencies.AssignmentsCount,
			"appeals_count":     info.Dependencies.AppealsCount,
			"usage_log_count":   info.Dependencies.UsageLogCount,
			"polygons_count":    info.Dependencies.PolygonsCount,
		},
		"will_be_deleted": gin.H{
			"tickets":     info.Dependencies.TicketsCount > 0,
			"trips":       info.Dependencies.TripsCount > 0,
			"assignments": info.Dependencies.AssignmentsCount > 0,
			"appeals":     info.Dependencies.AppealsCount > 0,
			"usage_log":   info.Dependencies.UsageLogCount > 0,
			"polygons":    info.Dependencies.PolygonsCount > 0,
		},
	}))
}

func (h *Handler) deleteContract(c *gin.Context) {
	principal, ok := middleware.MustPrincipal(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, errorResponse("missing principal"))
		return
	}

	contractID, err := parseUUIDParam(c, "id")
	if err != nil {
		c.JSON(http.StatusBadRequest, errorResponse("invalid contract id"))
		return
	}

	// Check force parameter for cascade deletion
	force := parseBoolQuery(c.Query("force"))

	if err := h.contracts.Delete(c.Request.Context(), principal, contractID, force); err != nil {
		h.handleError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrPermissionDenied):
		c.JSON(http.StatusForbidden, errorResponse(err.Error()))
	case errors.Is(err, service.ErrNotFound):
		c.JSON(http.StatusNotFound, errorResponse(err.Error()))
	case errors.Is(err, service.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, errorResponse(err.Error()))
	case errors.Is(err, service.ErrConflict):
		c.JSON(http.StatusConflict, errorResponse(err.Error()))
	default:
		h.log.Error().Err(err).Msg("handler error")
		c.JSON(http.StatusInternalServerError, errorResponse("internal error"))
	}
}

func parseBoolQuery(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseUUIDParam(c *gin.Context, param string) (uuid.UUID, error) {
	raw := strings.TrimSpace(c.Param(param))
	return uuid.Parse(raw)
}

func parseTime(raw string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	// Try common formats
	formats := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if t, err := time.Parse(format, raw); err == nil {
			return t, nil
		}
	}
	return time.Time{}, errors.New("invalid time format")
}

func successResponse(data interface{}) gin.H {
	return gin.H{
		"data": data,
	}
}

func errorResponse(message string) gin.H {
	return gin.H{
		"error": message,
	}
}
