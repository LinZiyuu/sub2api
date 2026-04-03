package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// AccountConnectivityProbeService periodically runs lightweight chat probes against active accounts.
// This complements TokenRefreshService: a valid refresh token does not guarantee upstream inference works.
type AccountConnectivityProbeService struct {
	accountRepo AccountRepository
	resultRepo  AccountConnectivityProbeResultRepository
	testSvc     *AccountTestService
	cfg         *config.Config

	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

// NewAccountConnectivityProbeService constructs a probe service.
func NewAccountConnectivityProbeService(
	accountRepo AccountRepository,
	resultRepo AccountConnectivityProbeResultRepository,
	testSvc *AccountTestService,
	cfg *config.Config,
) *AccountConnectivityProbeService {
	return &AccountConnectivityProbeService{
		accountRepo: accountRepo,
		resultRepo:  resultRepo,
		testSvc:     testSvc,
		cfg:         cfg,
		stopCh:      make(chan struct{}),
	}
}

// Start launches the background probe loop when enabled in config.
func (s *AccountConnectivityProbeService) Start() {
	if s == nil || s.cfg == nil || !s.cfg.AccountConnectivityProbe.Enabled {
		slog.Info("account_connectivity_probe.disabled")
		return
	}

	probe := &s.cfg.AccountConnectivityProbe
	interval := time.Duration(probe.IntervalMinutes) * time.Minute

	s.wg.Add(1)
	go s.loop(interval)

	slog.Info("account_connectivity_probe.started",
		"interval", interval,
		"max_concurrency", probe.MaxConcurrency,
		"per_account_timeout_seconds", probe.PerAccountTimeoutSeconds,
		"run_on_start", probe.RunOnStart,
		"persist_results", probe.PersistResults,
		"retention_days", probe.RetentionDays,
	)
}

// Stop shuts down the probe loop.
func (s *AccountConnectivityProbeService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	s.wg.Wait()
	slog.Info("account_connectivity_probe.stopped")
}

func (s *AccountConnectivityProbeService) loop(interval time.Duration) {
	defer s.wg.Done()

	if s.cfg.AccountConnectivityProbe.RunOnStart {
		s.runCycle()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runCycle()
		}
	}
}

func (s *AccountConnectivityProbeService) runCycle() {
	probe := &s.cfg.AccountConnectivityProbe
	ctx := context.Background()

	accounts, err := s.accountRepo.ListActive(ctx)
	if err != nil {
		slog.Error("account_connectivity_probe.list_active_failed", "error", err)
		return
	}
	if len(accounts) == 0 {
		return
	}

	sem := make(chan struct{}, probe.MaxConcurrency)
	var wg sync.WaitGroup

	for i := range accounts {
		acc := accounts[i]
		sem <- struct{}{}
		wg.Add(1)
		go func(a Account) {
			defer wg.Done()
			defer func() { <-sem }()
			s.probeOne(a)
		}(acc)
	}

	wg.Wait()

	slog.Info("account_connectivity_probe.cycle_completed", "accounts", len(accounts))

	if probe.PersistResults && probe.RetentionDays > 0 && s.resultRepo != nil {
		cutoff := time.Now().AddDate(0, 0, -probe.RetentionDays)
		n, delErr := s.resultRepo.DeleteOlderThan(context.Background(), cutoff)
		if delErr != nil {
			slog.Warn("account_connectivity_probe.retention_cleanup_failed", "error", delErr)
		} else if n > 0 {
			slog.Info("account_connectivity_probe.retention_cleanup", "deleted_rows", n, "cutoff", cutoff)
		}
	}
}

func (s *AccountConnectivityProbeService) probeOne(acc Account) {
	probe := &s.cfg.AccountConnectivityProbe
	timeout := time.Duration(probe.PerAccountTimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	model := resolveConnectivityProbeModel(&acc, &probe.Models)
	res, err := s.testSvc.RunTestBackground(ctx, acc.ID, model)

	insertCtx, insertCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer insertCancel()

	if err != nil {
		slog.Warn("account_connectivity_probe.request_failed",
			"account_id", acc.ID,
			"platform", acc.Platform,
			"error", err,
		)
		s.persist(insertCtx, acc, model, "error", err.Error(), 0, "")
		return
	}

	if res.Status != "success" {
		slog.Warn("account_connectivity_probe.upstream_failed",
			"account_id", acc.ID,
			"platform", acc.Platform,
			"model", model,
			"latency_ms", res.LatencyMs,
			"error_message", res.ErrorMessage,
		)
		s.persist(insertCtx, acc, model, "failed", res.ErrorMessage, res.LatencyMs, "")
		return
	}

	preview := truncateProbePreview(res.ResponseText, probe.ResponsePreviewMaxBytes)
	slog.Info("account_connectivity_probe.ok",
		"account_id", acc.ID,
		"platform", acc.Platform,
		"model", model,
		"latency_ms", res.LatencyMs,
	)
	s.persist(insertCtx, acc, model, "success", "", res.LatencyMs, preview)
}

func (s *AccountConnectivityProbeService) persist(ctx context.Context, acc Account, modelID, status, errMsg string, latencyMs int64, preview string) {
	if s.resultRepo == nil || s.cfg == nil || !s.cfg.AccountConnectivityProbe.PersistResults {
		return
	}
	row := &AccountConnectivityProbeResult{
		AccountID:       acc.ID,
		Platform:        acc.Platform,
		ModelID:         modelID,
		Status:          status,
		ErrorMessage:    errMsg,
		LatencyMs:       latencyMs,
		ResponsePreview: preview,
	}
	if err := s.resultRepo.Insert(ctx, row); err != nil {
		slog.Warn("account_connectivity_probe.persist_failed",
			"account_id", acc.ID,
			"status", status,
			"error", err,
		)
	}
}

func truncateProbePreview(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	out := string(b[:maxBytes])
	return out + "…"
}

func resolveConnectivityProbeModel(acc *Account, models *config.AccountConnectivityProbeModelsConfig) string {
	if acc == nil || models == nil {
		return ""
	}
	switch acc.Platform {
	case PlatformOpenAI:
		if acc.IsOpenAIOAuth() && models.OpenAIOAuth != "" {
			return models.OpenAIOAuth
		}
		if acc.IsOpenAIApiKey() && models.OpenAIAPIKey != "" {
			return models.OpenAIAPIKey
		}
		return ""
	case PlatformAnthropic:
		if models.Anthropic != "" {
			return models.Anthropic
		}
		return ""
	case PlatformGemini:
		if models.Gemini != "" {
			return models.Gemini
		}
		return ""
	case PlatformAntigravity:
		if models.Antigravity != "" {
			return models.Antigravity
		}
		return ""
	default:
		return ""
	}
}
