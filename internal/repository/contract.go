package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/nurpe/snowops-contract/internal/model"
)

var (
	ErrTicketAlreadyLinked = errors.New("ticket already linked to a different contract")
	ErrTicketNotLinked     = errors.New("ticket is not linked to any contract")
	ErrTicketNotFound      = errors.New("ticket not found")
	ErrTripUsageDuplicate  = errors.New("trip usage already recorded")
)

type ContractFilter struct {
	ContractorID *uuid.UUID
	LandfillID   *uuid.UUID
	ContractType *model.ContractType
	CreatedByOrg *uuid.UUID
	WorkType     *model.WorkType
	OnlyActive   bool
	IncludeUsage bool
	Status       *model.ContractUIStatus
	StartFrom    *time.Time
	StartTo      *time.Time
	EndFrom      *time.Time
	EndTo        *time.Time
	Now          time.Time
}

type ContractRepository struct {
	db *gorm.DB
}

func NewContractRepository(db *gorm.DB) *ContractRepository {
	return &ContractRepository{db: db}
}

func (r *ContractRepository) List(ctx context.Context, filter ContractFilter) ([]model.Contract, error) {
	query := r.db.WithContext(ctx).Table("contracts c").
		Select(`
			c.id,
			c.contractor_id,
			c.landfill_id,
			c.created_by_org AS created_by_org_id,
			c.contract_type,
			c.name,
			c.work_type,
			c.price_per_m3,
			c.budget_total,
			c.minimal_volume_m3,
			c.start_at,
			c.end_at,
			c.is_active,
			c.created_at,
			NULL::TIMESTAMPTZ AS updated_at
		`)

	if filter.ContractorID != nil {
		query = query.Where("c.contractor_id = ?", *filter.ContractorID)
	}
	if filter.LandfillID != nil {
		query = query.Where("c.landfill_id = ?", *filter.LandfillID)
	}
	if filter.ContractType != nil {
		query = query.Where("c.contract_type = ?", string(*filter.ContractType))
	}
	if filter.CreatedByOrg != nil {
		query = query.Where("c.created_by_org = ?", *filter.CreatedByOrg)
	}
	if filter.WorkType != nil {
		query = query.Where("c.work_type = ?", string(*filter.WorkType))
	}
	if filter.OnlyActive {
		query = query.Where("c.is_active = TRUE")
	}
	if filter.StartFrom != nil {
		query = query.Where("c.start_at >= ?", *filter.StartFrom)
	}
	if filter.StartTo != nil {
		query = query.Where("c.start_at <= ?", *filter.StartTo)
	}
	if filter.EndFrom != nil {
		query = query.Where("c.end_at >= ?", *filter.EndFrom)
	}
	if filter.EndTo != nil {
		query = query.Where("c.end_at <= ?", *filter.EndTo)
	}
	if filter.Status != nil {
		now := filter.Now
		if now.IsZero() {
			now = time.Now()
		}
		switch *filter.Status {
		case model.ContractUIStatusPlanned:
			query = query.Where("c.is_active = TRUE AND c.start_at > ?", now)
		case model.ContractUIStatusActive:
			query = query.Where("c.is_active = TRUE AND c.start_at <= ? AND c.end_at >= ?", now, now)
		case model.ContractUIStatusExpired:
			query = query.Where("c.is_active = TRUE AND c.end_at < ?", now)
		case model.ContractUIStatusArchived:
			query = query.Where("c.is_active = FALSE")
		}
	}

	query = query.Order("c.created_at DESC")

	var contracts []model.Contract
	if err := query.Scan(&contracts).Error; err != nil {
		return nil, err
	}

	if filter.IncludeUsage {
		for i := range contracts {
			usage, err := r.getUsage(ctx, contracts[i].ID)
			if err == nil {
				contracts[i].Usage = usage
			}
			// Загружаем polygon_ids для LANDFILL_SERVICE контрактов
			if contracts[i].ContractType == model.ContractTypeLandfillService {
				polygonIDs, err := r.GetPolygonIDs(ctx, contracts[i].ID)
				if err == nil {
					contracts[i].PolygonIDs = polygonIDs
				}
			}
		}
	}

	return contracts, nil
}

func (r *ContractRepository) GetByID(ctx context.Context, id uuid.UUID, includeUsage bool) (*model.Contract, error) {
	var contract model.Contract
	err := r.db.WithContext(ctx).
		Raw(`
			SELECT
				c.id,
				c.contractor_id,
				c.landfill_id,
				c.created_by_org AS created_by_org_id,
				c.contract_type,
				c.name,
				c.work_type,
				c.price_per_m3,
				c.budget_total,
				c.minimal_volume_m3,
				c.start_at,
				c.end_at,
				c.is_active,
				c.created_at,
				NULL::TIMESTAMPTZ AS updated_at
			FROM contracts c
			WHERE c.id = ?
			LIMIT 1
		`, id).Scan(&contract).Error
	if err != nil {
		return nil, err
	}
	if contract.ID == uuid.Nil {
		return nil, gorm.ErrRecordNotFound
	}

	if includeUsage {
		usage, err := r.getUsage(ctx, contract.ID)
		if err == nil {
			contract.Usage = usage
		}
	}

	// Загружаем polygon_ids для LANDFILL_SERVICE контрактов
	if contract.ContractType == model.ContractTypeLandfillService {
		polygonIDs, err := r.GetPolygonIDs(ctx, contract.ID)
		if err == nil {
			contract.PolygonIDs = polygonIDs
		}
	}

	return &contract, nil
}

func (r *ContractRepository) getUsage(ctx context.Context, contractID uuid.UUID) (*model.ContractUsage, error) {
	var usage model.ContractUsage
	err := r.db.WithContext(ctx).
		Raw(`
			SELECT
				id,
				contract_id,
				total_volume_m3,
				total_cost,
				updated_at
			FROM contract_usage
			WHERE contract_id = ?
			LIMIT 1
		`, contractID).Scan(&usage).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &usage, nil
}

type CreateContractParams struct {
	ContractorID    *uuid.UUID
	LandfillID      *uuid.UUID
	ContractType    model.ContractType
	CreatedByOrgID  uuid.UUID
	Name            string
	WorkType        model.WorkType
	PricePerM3      float64
	BudgetTotal     float64
	MinimalVolumeM3 float64
	StartAt         time.Time
	EndAt           time.Time
	IsActive        bool
	PolygonIDs      []uuid.UUID
}

func (r *ContractRepository) Create(ctx context.Context, params CreateContractParams) (*model.Contract, error) {
	var contract model.Contract
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO contracts (
			contractor_id,
			landfill_id,
			contract_type,
			created_by_org,
			name,
			work_type,
			price_per_m3,
			budget_total,
			minimal_volume_m3,
			start_at,
			end_at,
			is_active
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING
			id,
			contractor_id,
			landfill_id,
			created_by_org AS created_by_org_id,
			contract_type,
			name,
			work_type,
			price_per_m3,
			budget_total,
			minimal_volume_m3,
			start_at,
			end_at,
			is_active,
			created_at,
			NULL::TIMESTAMPTZ AS updated_at
	`, params.ContractorID, params.LandfillID, string(params.ContractType), params.CreatedByOrgID, params.Name, string(params.WorkType),
		params.PricePerM3, params.BudgetTotal, params.MinimalVolumeM3,
		params.StartAt, params.EndAt, params.IsActive).Scan(&contract).Error
	if err != nil {
		return nil, err
	}

	// Create initial usage record
	err = r.db.WithContext(ctx).Exec(`
		INSERT INTO contract_usage (contract_id, total_volume_m3, total_cost)
		VALUES (?, 0, 0)
		ON CONFLICT (contract_id) DO NOTHING
	`, contract.ID).Error
	if err != nil {
		return nil, err
	}

	// Сохраняем polygon_ids для LANDFILL_SERVICE контрактов
	if params.ContractType == model.ContractTypeLandfillService && len(params.PolygonIDs) > 0 {
		if err := r.SetPolygonIDs(ctx, contract.ID, params.PolygonIDs); err != nil {
			return nil, err
		}
		contract.PolygonIDs = params.PolygonIDs
	}

	return &contract, nil
}

func (r *ContractRepository) UpdateUsage(ctx context.Context, contractID uuid.UUID, volumeM3, cost float64) error {
	err := r.db.WithContext(ctx).Exec(`
		INSERT INTO contract_usage (contract_id, total_volume_m3, total_cost)
		VALUES (?, ?, ?)
		ON CONFLICT (contract_id)
		DO UPDATE SET
			total_volume_m3 = contract_usage.total_volume_m3 + EXCLUDED.total_volume_m3,
			total_cost = contract_usage.total_cost + EXCLUDED.total_cost,
			updated_at = NOW()
	`, contractID, volumeM3, cost).Error
	return err
}

func (r *ContractRepository) AssignTicketContract(ctx context.Context, ticketID, contractID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing struct {
			ContractID *uuid.UUID
		}
		err := tx.Raw(`SELECT contract_id FROM tickets WHERE id = ? FOR UPDATE`, ticketID).Scan(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTicketNotFound
		}
		if err != nil {
			return err
		}
		if existing.ContractID != nil {
			if *existing.ContractID == contractID {
				return nil
			}
			return ErrTicketAlreadyLinked
		}
		return tx.Exec(`UPDATE tickets SET contract_id = ? WHERE id = ?`, contractID, ticketID).Error
	})
}

func (r *ContractRepository) GetContractIDByTicket(ctx context.Context, ticketID uuid.UUID) (uuid.UUID, error) {
	var contractID uuid.UUID
	err := r.db.WithContext(ctx).Raw(`
		SELECT contract_id FROM tickets WHERE id = ?
	`, ticketID).Scan(&contractID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, ErrTicketNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if contractID == uuid.Nil {
		return uuid.Nil, ErrTicketNotLinked
	}
	return contractID, nil
}

type TripUsageParams struct {
	TripID     uuid.UUID
	TicketID   uuid.UUID
	VolumeM3   float64
	ContractID uuid.UUID
}

func (r *ContractRepository) RecordTripUsage(ctx context.Context, params TripUsageParams, pricePerM3 float64) error {
	cost := params.VolumeM3 * pricePerM3
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO trip_usage_log (trip_id, ticket_id, contract_id, recorded_volume_m3, recorded_cost)
			VALUES (?, ?, ?, ?, ?)
		`, params.TripID, params.TicketID, params.ContractID, params.VolumeM3, cost).Error; err != nil {
			if errors.Is(err, gorm.ErrDuplicatedKey) {
				return ErrTripUsageDuplicate
			}
			return err
		}
		return tx.Exec(`
			INSERT INTO contract_usage (contract_id, total_volume_m3, total_cost)
			VALUES (?, ?, ?)
			ON CONFLICT (contract_id)
			DO UPDATE SET
				total_volume_m3 = contract_usage.total_volume_m3 + EXCLUDED.total_volume_m3,
				total_cost = contract_usage.total_cost + EXCLUDED.total_cost,
				updated_at = NOW()
		`, params.ContractID, params.VolumeM3, cost).Error
	})
}

func (r *ContractRepository) ListContractTickets(ctx context.Context, contractID uuid.UUID) ([]model.ContractTicket, error) {
	var items []model.ContractTicket
	err := r.db.WithContext(ctx).Raw(`
		WITH trip_agg AS (
			SELECT
				ticket_id,
				COUNT(*) AS trip_count,
				COALESCE(SUM(COALESCE(detected_volume_entry, 0)), 0) AS total_volume_m3
			FROM trips
			WHERE ticket_id IS NOT NULL
			GROUP BY ticket_id
		),
		assign_agg AS (
			SELECT
				ticket_id,
				COUNT(*) AS active_assignments
			FROM ticket_assignments
			WHERE is_active = TRUE
			GROUP BY ticket_id
		)
		SELECT
			t.id,
			t.cleaning_area_id,
			ca.name AS cleaning_area_name,
			t.planned_start_at,
			t.planned_end_at,
			t.status,
			COALESCE(trip_agg.trip_count, 0) AS trip_count,
			COALESCE(trip_agg.total_volume_m3, 0) AS total_volume_m3,
			COALESCE(assign_agg.active_assignments, 0) AS active_assignments
		FROM tickets t
		LEFT JOIN cleaning_areas ca ON ca.id = t.cleaning_area_id
		LEFT JOIN trip_agg ON trip_agg.ticket_id = t.id
		LEFT JOIN assign_agg ON assign_agg.ticket_id = t.id
		WHERE t.contract_id = ?
		ORDER BY t.planned_start_at DESC
	`, contractID).Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *ContractRepository) ListContractTrips(ctx context.Context, contractID uuid.UUID) ([]model.ContractTrip, error) {
	var items []model.ContractTrip
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			tr.id,
			tr.ticket_id,
			tr.ticket_assignment_id,
			tr.driver_id,
			tr.vehicle_id,
			tr.camera_id,
			tr.polygon_id,
			tr.vehicle_plate_number,
			tr.detected_plate_number,
			tr.entry_at,
			tr.exit_at,
			tr.status,
			tr.detected_volume_entry,
			tr.detected_volume_exit
		FROM trips tr
		JOIN tickets t ON t.id = tr.ticket_id
		WHERE t.contract_id = ?
		ORDER BY tr.entry_at DESC
	`, contractID).Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// GetPolygonIDs возвращает список polygon_id для контракта
func (r *ContractRepository) GetPolygonIDs(ctx context.Context, contractID uuid.UUID) ([]uuid.UUID, error) {
	var polygonIDs []uuid.UUID
	err := r.db.WithContext(ctx).
		Raw(`
			SELECT polygon_id
			FROM contract_polygons
			WHERE contract_id = ?
			ORDER BY polygon_id
		`, contractID).Scan(&polygonIDs).Error
	if err != nil {
		return nil, err
	}
	return polygonIDs, nil
}

// SetPolygonIDs устанавливает список polygon_id для контракта
func (r *ContractRepository) SetPolygonIDs(ctx context.Context, contractID uuid.UUID, polygonIDs []uuid.UUID) error {
	// Удаляем существующие связи
	if err := r.db.WithContext(ctx).Exec(`
		DELETE FROM contract_polygons
		WHERE contract_id = ?
	`, contractID).Error; err != nil {
		return err
	}

	// Добавляем новые связи
	if len(polygonIDs) > 0 {
		for _, polygonID := range polygonIDs {
			if err := r.db.WithContext(ctx).Exec(`
				INSERT INTO contract_polygons (contract_id, polygon_id)
				VALUES (?, ?)
				ON CONFLICT (contract_id, polygon_id) DO NOTHING
			`, contractID, polygonID).Error; err != nil {
				return err
			}
		}
	}

	return nil
}

// Delete deletes a contract by ID
func (r *ContractRepository) Delete(ctx context.Context, id uuid.UUID) error {
	result := r.db.WithContext(ctx).
		Table("contracts").
		Where("id = ?", id).
		Delete(nil)

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// HasRelatedTickets checks if a contract has linked tickets
func (r *ContractRepository) HasRelatedTickets(ctx context.Context, contractID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("tickets").
		Where("contract_id = ?", contractID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// ContractDependencies contains dependency counts for a contract
type ContractDependencies struct {
	TicketsCount     int64 `json:"tickets_count"`
	TripsCount       int64 `json:"trips_count"`
	AssignmentsCount int64 `json:"assignments_count"`
	AppealsCount     int64 `json:"appeals_count"`
	UsageLogCount    int64 `json:"usage_log_count"`
	PolygonsCount    int64 `json:"polygons_count"`
}

// GetDependencies returns dependency counts for a contract
func (r *ContractRepository) GetDependencies(ctx context.Context, contractID uuid.UUID) (*ContractDependencies, error) {
	var deps ContractDependencies

	// Count tickets
	if err := r.db.WithContext(ctx).
		Table("tickets").
		Where("contract_id = ?", contractID).
		Count(&deps.TicketsCount).Error; err != nil {
		return nil, err
	}

	// Count trips via tickets
	if err := r.db.WithContext(ctx).
		Table("trips").
		Joins("JOIN tickets ON tickets.id = trips.ticket_id").
		Where("tickets.contract_id = ?", contractID).
		Count(&deps.TripsCount).Error; err != nil {
		return nil, err
	}

	// Count ticket assignments via tickets
	if err := r.db.WithContext(ctx).
		Table("ticket_assignments").
		Joins("JOIN tickets ON tickets.id = ticket_assignments.ticket_id").
		Where("tickets.contract_id = ?", contractID).
		Count(&deps.AssignmentsCount).Error; err != nil {
		return nil, err
	}

	// Count appeals via tickets
	if err := r.db.WithContext(ctx).
		Table("appeals").
		Joins("JOIN tickets ON tickets.id = appeals.ticket_id").
		Where("tickets.contract_id = ? AND appeals.ticket_id IS NOT NULL", contractID).
		Count(&deps.AppealsCount).Error; err != nil {
		return nil, err
	}

	// Count trip usage log entries
	if err := r.db.WithContext(ctx).
		Table("trip_usage_log").
		Where("contract_id = ?", contractID).
		Count(&deps.UsageLogCount).Error; err != nil {
		return nil, err
	}

	// Count polygons
	if err := r.db.WithContext(ctx).
		Table("contract_polygons").
		Where("contract_id = ?", contractID).
		Count(&deps.PolygonsCount).Error; err != nil {
		return nil, err
	}

	return &deps, nil
}

// DeleteTicketsByContractID deletes all tickets linked to a contract
// This is used when force=true to allow contract deletion
func (r *ContractRepository) DeleteTicketsByContractID(ctx context.Context, contractID uuid.UUID) error {
	// Deleting tickets will cascade to:
	// - ticket_assignments (ON DELETE CASCADE)
	// - appeals (ON DELETE CASCADE)
	// trips.ticket_id will be set to NULL (ON DELETE SET NULL)
	result := r.db.WithContext(ctx).
		Table("tickets").
		Where("contract_id = ?", contractID).
		Delete(nil)

	return result.Error
}
