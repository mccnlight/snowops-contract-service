package db

import (
	"fmt"

	"gorm.io/gorm"
)

var migrationStatements = []string{
	`CREATE EXTENSION IF NOT EXISTS "uuid-ossp";`,
	`CREATE EXTENSION IF NOT EXISTS "pgcrypto";`,
	`CREATE TABLE IF NOT EXISTS contracts (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		contractor_id UUID NOT NULL,
		created_by_org UUID NOT NULL,
		name VARCHAR(255) NOT NULL,
		work_type VARCHAR(50) NOT NULL,
		price_per_m3 NUMERIC(10,2) NOT NULL,
		budget_total NUMERIC(14,2) NOT NULL,
		minimal_volume_m3 NUMERIC(14,2) NOT NULL,
		start_at TIMESTAMPTZ NOT NULL,
		end_at TIMESTAMPTZ NOT NULL,
		is_active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_contractor_id ON contracts (contractor_id);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_created_by_org ON contracts (created_by_org);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_work_type ON contracts (work_type);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_is_active ON contracts (is_active);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_start_at ON contracts (start_at);`,
	`CREATE INDEX IF NOT EXISTS idx_contracts_end_at ON contracts (end_at);`,
	`CREATE TABLE IF NOT EXISTS contract_usage (
		id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
		contract_id UUID NOT NULL UNIQUE REFERENCES contracts(id) ON DELETE CASCADE,
		total_volume_m3 NUMERIC(14,2) NOT NULL DEFAULT 0,
		total_cost NUMERIC(14,2) NOT NULL DEFAULT 0,
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`,
	`CREATE INDEX IF NOT EXISTS idx_contract_usage_contract_id ON contract_usage (contract_id);`,
	`CREATE TABLE IF NOT EXISTS trip_usage_log (
		trip_id UUID PRIMARY KEY,
		ticket_id UUID NOT NULL,
		contract_id UUID NOT NULL REFERENCES contracts(id) ON DELETE CASCADE,
		recorded_volume_m3 NUMERIC(10,2) NOT NULL CHECK (recorded_volume_m3 > 0),
		recorded_cost NUMERIC(14,2) NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);`,
	`CREATE INDEX IF NOT EXISTS idx_trip_usage_log_contract_id ON trip_usage_log (contract_id);`,
	`CREATE OR REPLACE FUNCTION set_updated_at()
	RETURNS TRIGGER AS $$
	BEGIN
		NEW.updated_at = NOW();
		RETURN NEW;
	END;
	$$ LANGUAGE plpgsql;`,
	`DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_contract_usage_updated_at') THEN
			CREATE TRIGGER trg_contract_usage_updated_at
				BEFORE UPDATE ON contract_usage
				FOR EACH ROW
				EXECUTE PROCEDURE set_updated_at();
		END IF;
	END
	$$;`,
}

func runMigrations(db *gorm.DB) error {
	for i, stmt := range migrationStatements {
		if err := db.Exec(stmt).Error; err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}
	return nil
}
