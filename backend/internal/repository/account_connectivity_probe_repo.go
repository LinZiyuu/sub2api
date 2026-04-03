package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type accountConnectivityProbeResultRepository struct {
	db *sql.DB
}

// NewAccountConnectivityProbeResultRepository creates the probe result store.
func NewAccountConnectivityProbeResultRepository(db *sql.DB) service.AccountConnectivityProbeResultRepository {
	return &accountConnectivityProbeResultRepository{db: db}
}

func (r *accountConnectivityProbeResultRepository) Insert(ctx context.Context, row *service.AccountConnectivityProbeResult) error {
	if row == nil {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO account_connectivity_probe_results (account_id, platform, model_id, status, error_message, latency_ms, response_preview, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
	`, row.AccountID, row.Platform, row.ModelID, row.Status, row.ErrorMessage, row.LatencyMs, row.ResponsePreview)
	return err
}

func (r *accountConnectivityProbeResultRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM account_connectivity_probe_results WHERE created_at < $1
	`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return n, err
}
