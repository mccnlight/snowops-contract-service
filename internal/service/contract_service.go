package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nurpe/snowops-contract/internal/model"
	"github.com/nurpe/snowops-contract/internal/repository"
)

type ContractService struct {
	contracts *repository.ContractRepository
	now       func() time.Time
}

func NewContractService(contracts *repository.ContractRepository) *ContractService {
	return &ContractService{
		contracts: contracts,
		now:       time.Now,
	}
}

type ListContractsInput struct {
	ContractorID *uuid.UUID
	WorkType     *model.WorkType
	OnlyActive   bool
	Status       *model.ContractUIStatus
	StartFrom    *time.Time
	StartTo      *time.Time
	EndFrom      *time.Time
	EndTo        *time.Time
}

func (s *ContractService) List(ctx context.Context, principal model.Principal, input ListContractsInput) ([]model.Contract, error) {
	filter := repository.ContractFilter{
		OnlyActive:   input.OnlyActive && input.Status == nil,
		IncludeUsage: true,
		Status:       input.Status,
		StartFrom:    input.StartFrom,
		StartTo:      input.StartTo,
		EndFrom:      input.EndFrom,
		EndTo:        input.EndTo,
		Now:          s.now(),
	}

	switch {
	case principal.IsContractor():
		filter.ContractorID = &principal.OrganizationID
	case principal.IsKgu(), principal.IsAkimat():
		if input.ContractorID != nil {
			filter.ContractorID = input.ContractorID
		}
	default:
		return nil, ErrPermissionDenied
	}

	if input.WorkType != nil {
		filter.WorkType = input.WorkType
	}

	contracts, err := s.contracts.List(ctx, filter)
	if err != nil {
		return nil, err
	}

	for i := range contracts {
		s.decorateContract(&contracts[i])
	}

	return contracts, nil
}

func (s *ContractService) Get(ctx context.Context, principal model.Principal, id uuid.UUID) (*model.Contract, error) {
	contract, err := s.contracts.GetByID(ctx, id, true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := s.ensureReadAccess(principal, contract); err != nil {
		return nil, err
	}

	s.decorateContract(contract)
	return contract, nil
}

type CreateContractInput struct {
	ContractorID    uuid.UUID
	Name            string
	WorkType        model.WorkType
	PricePerM3      float64
	BudgetTotal     float64
	MinimalVolumeM3 float64
	StartAt         time.Time
	EndAt           time.Time
	IsActive        *bool
}

func (s *ContractService) Create(ctx context.Context, principal model.Principal, input CreateContractInput) (*model.Contract, error) {
	if !principal.IsKgu() {
		return nil, ErrPermissionDenied
	}

	if strings.TrimSpace(input.Name) == "" {
		return nil, ErrInvalidInput
	}
	if input.PricePerM3 <= 0 {
		return nil, ErrInvalidInput
	}
	if input.BudgetTotal <= 0 {
		return nil, ErrInvalidInput
	}
	if input.MinimalVolumeM3 <= 0 {
		return nil, ErrInvalidInput
	}
	if !input.EndAt.After(input.StartAt) {
		return nil, ErrInvalidInput
	}

	if input.WorkType != model.WorkTypeRoad &&
		input.WorkType != model.WorkTypeSidewalk &&
		input.WorkType != model.WorkTypeYard {
		return nil, ErrInvalidInput
	}

	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}

	params := repository.CreateContractParams{
		ContractorID:    input.ContractorID,
		CreatedByOrgID:  principal.OrganizationID,
		Name:            strings.TrimSpace(input.Name),
		WorkType:        input.WorkType,
		PricePerM3:      input.PricePerM3,
		BudgetTotal:     input.BudgetTotal,
		MinimalVolumeM3: input.MinimalVolumeM3,
		StartAt:         input.StartAt,
		EndAt:           input.EndAt,
		IsActive:        isActive,
	}

	contract, err := s.contracts.Create(ctx, params)
	if err != nil {
		return nil, err
	}

	s.decorateContract(contract)
	return contract, nil
}

func (s *ContractService) decorateContract(contract *model.Contract) {
	now := s.now()
	status := deriveUIStatus(contract, now)
	contract.UIStatus = status

	usageVolume := 0.0
	usageCost := 0.0
	if contract.Usage != nil {
		usageVolume = contract.Usage.TotalVolumeM3
		usageCost = contract.Usage.TotalCost
	}

	if contract.MinimalVolumeM3 > 0 {
		contract.VolumeProgress = usageVolume / contract.MinimalVolumeM3
	}

	contract.PayableAmount = math.Min(usageCost, contract.BudgetTotal)
	if usageCost > contract.BudgetTotal {
		contract.BudgetExceeded = true
	}

	switch status {
	case model.ContractUIStatusExpired:
		if usageVolume >= contract.MinimalVolumeM3 {
			contract.Result = model.ContractResultSuccess
		} else {
			contract.Result = model.ContractResultFail
		}
	case model.ContractUIStatusPlanned, model.ContractUIStatusActive, model.ContractUIStatusArchived:
		contract.Result = model.ContractResultNone
	}
}

func deriveUIStatus(contract *model.Contract, now time.Time) model.ContractUIStatus {
	if !contract.IsActive {
		return model.ContractUIStatusArchived
	}
	if now.Before(contract.StartAt) {
		return model.ContractUIStatusPlanned
	}
	if now.After(contract.EndAt) {
		return model.ContractUIStatusExpired
	}
	return model.ContractUIStatusActive
}

type AssignTicketContractInput struct {
	TicketID   uuid.UUID
	ContractID uuid.UUID
}

func (s *ContractService) AssignTicketContract(ctx context.Context, principal model.Principal, input AssignTicketContractInput) error {
	if !principal.IsKgu() {
		return ErrPermissionDenied
	}
	contract, err := s.contracts.GetByID(ctx, input.ContractID, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if contract.CreatedByOrgID != principal.OrganizationID {
		return ErrPermissionDenied
	}
	err = s.contracts.AssignTicketContract(ctx, input.TicketID, input.ContractID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrTicketAlreadyLinked):
		return ErrConflict
	case errors.Is(err, repository.ErrTicketNotFound):
		return ErrNotFound
	default:
		return err
	}
}

type RecordTripUsageInput struct {
	TripID   uuid.UUID
	TicketID uuid.UUID
	VolumeM3 float64
}

func (s *ContractService) RecordTripUsage(ctx context.Context, principal model.Principal, input RecordTripUsageInput) error {
	if !(principal.IsKgu() || principal.IsAkimat()) {
		return ErrPermissionDenied
	}
	if input.VolumeM3 <= 0 {
		return ErrInvalidInput
	}

	contractID, err := s.contracts.GetContractIDByTicket(ctx, input.TicketID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrTicketNotFound):
			return ErrNotFound
		case errors.Is(err, repository.ErrTicketNotLinked):
			return ErrInvalidInput
		default:
			return err
		}
	}

	contract, err := s.contracts.GetByID(ctx, contractID, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}

	params := repository.TripUsageParams{
		TripID:     input.TripID,
		TicketID:   input.TicketID,
		VolumeM3:   input.VolumeM3,
		ContractID: contractID,
	}

	err = s.contracts.RecordTripUsage(ctx, params, contract.PricePerM3)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrTripUsageDuplicate):
			return ErrConflict
		default:
			return err
		}
	}
	return nil
}

func (s *ContractService) ListContractTickets(ctx context.Context, principal model.Principal, contractID uuid.UUID) ([]model.ContractTicket, error) {
	contract, err := s.contracts.GetByID(ctx, contractID, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.ensureReadAccess(principal, contract); err != nil {
		return nil, err
	}
	return s.contracts.ListContractTickets(ctx, contractID)
}

func (s *ContractService) ListContractTrips(ctx context.Context, principal model.Principal, contractID uuid.UUID) ([]model.ContractTrip, error) {
	contract, err := s.contracts.GetByID(ctx, contractID, false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.ensureReadAccess(principal, contract); err != nil {
		return nil, err
	}
	return s.contracts.ListContractTrips(ctx, contractID)
}

func (s *ContractService) ensureReadAccess(principal model.Principal, contract *model.Contract) error {
	switch {
	case principal.IsContractor():
		if contract.ContractorID != principal.OrganizationID {
			return ErrPermissionDenied
		}
	case principal.IsKgu(), principal.IsAkimat():
		// allowed
	default:
		return ErrPermissionDenied
	}
	return nil
}
