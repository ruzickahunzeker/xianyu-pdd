package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/auth"
)

var errOrderSyncRunning = errors.New("订单同步正在执行")

const (
	defaultOrderSyncInterval = 10
	minOrderSyncInterval     = 5
	maxOrderSyncInterval     = 1440
)

func (s *Server) executeOrderSync(ctx context.Context, userID int64, trigger, cookieID, statusFilter string) (map[string]any, int, error) {
	s.orderSyncMu.Lock()
	if s.orderSyncRunning[userID] {
		s.orderSyncMu.Unlock()
		return nil, http.StatusConflict, errOrderSyncRunning
	}
	s.orderSyncRunning[userID] = true
	s.orderSyncMu.Unlock()
	defer func() {
		s.orderSyncMu.Lock()
		delete(s.orderSyncRunning, userID)
		s.orderSyncMu.Unlock()
	}()

	started := time.Now().Unix()
	result, statusCode, syncErr := s.runOrderRefresh(ctx, userID, cookieID, statusFilter)
	finished := time.Now().Unix()
	summary := map[string]int{}
	if result != nil {
		if value, ok := result["summary"].(map[string]int); ok {
			summary = value
		}
	}
	fulfillmentUpdated := 0
	if syncErr == nil {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/internal/order-sync/reconcile", nil)
		fulfillmentUpdated, syncErr = s.reconcileUserFulfillments(request, userID)
		if syncErr != nil {
			statusCode = http.StatusInternalServerError
		}
	}
	runStatus := "success"
	errorMessage := ""
	if syncErr != nil {
		runStatus, errorMessage = "failed", syncErr.Error()
	} else if summary["failed"] > 0 {
		runStatus = "partial"
	}
	_, _ = s.Store.DB.ExecContext(ctx, `INSERT INTO order_sync_runs(user_id,trigger_type,status,started_at,finished_at,discovered,updated,soft_deleted,fulfillment_updated,failed,error_message) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		userID, trigger, runStatus, started, finished, summary["discovered"], summary["updated"], summary["soft_deleted"], fulfillmentUpdated, summary["failed"], errorMessage)
	if result != nil {
		result["fulfillment_updated"] = fulfillmentUpdated
		result["trigger_type"] = trigger
	}
	return result, statusCode, syncErr
}

func orderSyncInterval(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minOrderSyncInterval {
		return defaultOrderSyncInterval
	}
	if value > maxOrderSyncInterval {
		return maxOrderSyncInterval
	}
	return value
}

func settingEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (s *Server) RunOrderSyncScheduler(ctx context.Context) {
	startedAt := time.Now()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			enabledRaw, err := s.Store.Settings.Get(ctx, "order_sync_enabled")
			if err != nil || !settingEnabled(enabledRaw) {
				continue
			}
			intervalRaw, _ := s.Store.Settings.Get(ctx, "order_sync_interval_minutes")
			interval := time.Duration(orderSyncInterval(intervalRaw)) * time.Minute
			rows, err := s.Store.DB.QueryContext(ctx, `SELECT DISTINCT c.user_id FROM cookies c JOIN cookie_status cs ON cs.cookie_id=c.id WHERE cs.enabled=1`)
			if err != nil {
				s.Logger.Warn("扫描自动订单同步用户失败", "err", err)
				continue
			}
			userIDs := []int64{}
			for rows.Next() {
				var userID int64
				if rows.Scan(&userID) == nil {
					userIDs = append(userIDs, userID)
				}
			}
			_ = rows.Close()
			for _, userID := range userIDs {
				var lastFinished int64
				_ = s.Store.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(finished_at),0) FROM order_sync_runs WHERE user_id=?`, userID).Scan(&lastFinished)
				if lastFinished == 0 && time.Since(startedAt) < 30*time.Second {
					continue
				}
				if lastFinished > 0 && time.Since(time.Unix(lastFinished, 0)) < interval {
					continue
				}
				jobCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
				_, _, runErr := s.executeOrderSync(jobCtx, userID, "scheduled", "", "")
				cancel()
				if runErr != nil && !errors.Is(runErr, errOrderSyncRunning) {
					s.Logger.Warn("自动订单同步失败", "user_id", userID, "err", runErr)
				}
			}
		}
	}
}

func (s *Server) StartOrderSyncScheduler(ctx context.Context) {
	s.recoveryWG.Add(1)
	go func() {
		defer s.recoveryWG.Done()
		s.RunOrderSyncScheduler(ctx)
	}()
}

func (s *Server) orderSyncStatus(w http.ResponseWriter, r *http.Request) {
	userID := auth.SessionFromContext(r.Context()).UserID
	enabledRaw, _ := s.Store.Settings.Get(r.Context(), "order_sync_enabled")
	intervalRaw, _ := s.Store.Settings.Get(r.Context(), "order_sync_interval_minutes")
	interval := orderSyncInterval(intervalRaw)
	var trigger, status, errorMessage string
	var started, finished int64
	var discovered, updated, softDeleted, fulfillmentUpdated, failed int
	err := s.Store.DB.QueryRowContext(r.Context(), `SELECT trigger_type,status,started_at,finished_at,discovered,updated,soft_deleted,fulfillment_updated,failed,error_message FROM order_sync_runs WHERE user_id=? ORDER BY started_at DESC LIMIT 1`, userID).Scan(&trigger, &status, &started, &finished, &discovered, &updated, &softDeleted, &fulfillmentUpdated, &failed, &errorMessage)
	lastRun := any(nil)
	if err == nil {
		lastRun = map[string]any{"trigger_type": trigger, "status": status, "started_at": started, "finished_at": finished, "discovered": discovered, "updated": updated, "soft_deleted": softDeleted, "fulfillment_updated": fulfillmentUpdated, "failed": failed, "error_message": errorMessage}
	}
	s.orderSyncMu.Lock()
	running := s.orderSyncRunning[userID]
	s.orderSyncMu.Unlock()
	nextRun := int64(0)
	if settingEnabled(enabledRaw) {
		if finished > 0 {
			nextRun = time.Unix(finished, 0).Add(time.Duration(interval) * time.Minute).Unix()
		}
		if nextRun == 0 {
			nextRun = time.Now().Add(30 * time.Second).Unix()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": settingEnabled(enabledRaw), "interval_minutes": interval, "running": running, "next_run_at": nextRun, "last_run": lastRun})
}
