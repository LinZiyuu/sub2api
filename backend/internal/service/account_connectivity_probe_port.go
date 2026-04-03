package service

import (
	"context"
	"time"
)

// AccountConnectivityProbeResult is one persisted probe row (success or failure).
type AccountConnectivityProbeResult struct {
	ID              int64
	AccountID       int64
	Platform        string
	ModelID         string
	Status          string
	ErrorMessage    string
	LatencyMs       int64
	ResponsePreview string
	CreatedAt       time.Time
}

// AccountConnectivityProbeResultRepository persists probe outcomes for ops queries.
type AccountConnectivityProbeResultRepository interface {
	Insert(ctx context.Context, row *AccountConnectivityProbeResult) error
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
