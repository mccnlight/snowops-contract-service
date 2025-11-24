package model

import (
	"time"

	"github.com/google/uuid"
)

type UserRole string

const (
	UserRoleAkimatAdmin     UserRole = "AKIMAT_ADMIN"
	UserRoleKguZkhAdmin     UserRole = "KGU_ZKH_ADMIN"
	UserRoleTooAdmin        UserRole = "TOO_ADMIN" // Deprecated: use LANDFILL_ADMIN
	UserRoleLandfillAdmin   UserRole = "LANDFILL_ADMIN"
	UserRoleLandfillUser    UserRole = "LANDFILL_USER"
	UserRoleContractorAdmin UserRole = "CONTRACTOR_ADMIN"
	UserRoleDriver          UserRole = "DRIVER"
)

type WorkType string

const (
	WorkTypeRoad     WorkType = "road"
	WorkTypeSidewalk WorkType = "sidewalk"
	WorkTypeYard     WorkType = "yard"
)

type ContractType string

const (
	ContractTypeContractorService ContractType = "CONTRACTOR_SERVICE"
	ContractTypeLandfillService   ContractType = "LANDFILL_SERVICE"
)

type Contract struct {
	ID              uuid.UUID    `json:"id"`
	ContractorID    *uuid.UUID   `json:"contractor_id,omitempty"` // Опционально для LANDFILL_SERVICE
	LandfillID      *uuid.UUID   `json:"landfill_id,omitempty"`   // Для LANDFILL_SERVICE
	CreatedByOrgID  uuid.UUID    `json:"created_by_org_id"`
	ContractType    ContractType `json:"contract_type"`
	Name            string       `json:"name"`
	WorkType        WorkType     `json:"work_type"`
	PricePerM3      float64      `json:"price_per_m3"`
	BudgetTotal     float64      `json:"budget_total"`
	MinimalVolumeM3 float64      `json:"minimal_volume_m3"`
	StartAt         time.Time    `json:"start_at"`
	EndAt           time.Time    `json:"end_at"`
	IsActive        bool         `json:"is_active"`
	CreatedAt       time.Time    `json:"created_at"`
	UpdatedAt       *time.Time   `json:"updated_at,omitempty"`

	// Relations
	ContractorOrg  *OrganizationLookup `json:"contractor,omitempty" gorm:"-"`
	LandfillOrg    *OrganizationLookup `json:"landfill,omitempty" gorm:"-"`
	CreatedByOrg   *OrganizationLookup `json:"created_by_org,omitempty" gorm:"-"`
	PolygonIDs     []uuid.UUID         `json:"polygon_ids,omitempty" gorm:"-"` // Для LANDFILL_SERVICE
	Usage          *ContractUsage      `json:"usage,omitempty" gorm:"-"`
	UIStatus       ContractUIStatus    `json:"ui_status" gorm:"-"`
	Result         ContractResult      `json:"result" gorm:"-"`
	PayableAmount  float64             `json:"payable_amount" gorm:"-"`
	BudgetExceeded bool                `json:"budget_exceeded" gorm:"-"`
	VolumeProgress float64             `json:"volume_progress" gorm:"-"`
}

type ContractUsage struct {
	ID            uuid.UUID `json:"id"`
	ContractID    uuid.UUID `json:"contract_id"`
	TotalVolumeM3 float64   `json:"total_volume_m3"`
	TotalCost     float64   `json:"total_cost"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TicketStatus string

type ContractTicket struct {
	ID                uuid.UUID    `json:"id"`
	CleaningAreaID    uuid.UUID    `json:"cleaning_area_id"`
	CleaningAreaName  *string      `json:"cleaning_area_name,omitempty"`
	PlannedStartAt    time.Time    `json:"planned_start_at"`
	PlannedEndAt      time.Time    `json:"planned_end_at"`
	Status            TicketStatus `json:"status"`
	TripCount         int64        `json:"trip_count"`
	TotalVolumeM3     float64      `json:"total_volume_m3"`
	ActiveAssignments int64        `json:"active_assignments"`
}

type ContractTrip struct {
	ID                 uuid.UUID  `json:"id"`
	TicketID           uuid.UUID  `json:"ticket_id"`
	TicketAssignmentID *uuid.UUID `json:"ticket_assignment_id,omitempty"`
	DriverID           *uuid.UUID `json:"driver_id,omitempty"`
	VehicleID          *uuid.UUID `json:"vehicle_id,omitempty"`
	CameraID           *uuid.UUID `json:"camera_id,omitempty"`
	PolygonID          *uuid.UUID `json:"polygon_id,omitempty"`
	VehiclePlateNumber *string    `json:"vehicle_plate_number,omitempty"`
	DetectedPlate      *string    `json:"detected_plate_number,omitempty"`
	EntryAt            time.Time  `json:"entry_at"`
	ExitAt             *time.Time `json:"exit_at,omitempty"`
	Status             string     `json:"status"`
	VolumeEntry        *float64   `json:"detected_volume_entry,omitempty"`
	VolumeExit         *float64   `json:"detected_volume_exit,omitempty"`
}

type OrganizationLookup struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Principal struct {
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	Role           UserRole
}

func (p Principal) IsAkimat() bool {
	return p.Role == UserRoleAkimatAdmin
}

func (p Principal) IsKgu() bool {
	return p.Role == UserRoleKguZkhAdmin
}

func (p Principal) IsToo() bool {
	return p.Role == UserRoleTooAdmin
}

// IsLandfill проверяет, является ли пользователь администратором или пользователем полигона
// Также поддерживает обратную совместимость с TOO_ADMIN
func (p Principal) IsLandfill() bool {
	return p.Role == UserRoleLandfillAdmin || p.Role == UserRoleLandfillUser || p.Role == UserRoleTooAdmin
}

func (p Principal) IsContractor() bool {
	return p.Role == UserRoleContractorAdmin
}

func (p Principal) IsDriver() bool {
	return p.Role == UserRoleDriver
}

type ContractUIStatus string

const (
	ContractUIStatusPlanned  ContractUIStatus = "PLANNED"
	ContractUIStatusActive   ContractUIStatus = "ACTIVE"
	ContractUIStatusExpired  ContractUIStatus = "EXPIRED"
	ContractUIStatusArchived ContractUIStatus = "ARCHIVED"
)

type ContractResult string

const (
	ContractResultNone    ContractResult = "NONE"
	ContractResultSuccess ContractResult = "SUCCESS"
	ContractResultFail    ContractResult = "FAIL"
)
