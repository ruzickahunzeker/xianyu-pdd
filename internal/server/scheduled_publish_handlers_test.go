package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

func seedScheduledMaterial(t *testing.T, store *db.Store, salePrice int64) int64 {
	t.Helper()
	if _, err := store.DB.Exec(`INSERT INTO cookies(id,value,user_id) VALUES('scheduled-account','cookie',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO pdd_products(id,goods_id,title,images_json,first_collected_at,last_collected_at) VALUES(1,'123','来源','[]',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.Exec(`INSERT INTO pdd_skus(product_id,goods_id,sku_id,specs_json,spec_value_ids_json,thumb_url,prices_json,price_cent,stock,is_onsale,raw_snapshot_json,last_collected_at) VALUES(1,'123','456','[]','[]','','{}',100,8,1,'{}',20)`); err != nil {
		t.Fatal(err)
	}
	skus, _ := json.Marshal([]materialSKU{{MaterialSKUID: "stable", SKUType: materialSKUTypeSource, SourceGoodsID: "123", SourceSKUID: "456", PriceCents: salePrice, Quantity: 1, Enabled: true, Properties: []materialProperty{{Name: "款式", Value: "A"}}}})
	result, err := store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at,revision) VALUES(1,'pdd','123','定时素材','描述','["https://img.pddpic.com/1.jpg"]','{}',?,'draft',1,1,1)`, string(skus))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := result.LastInsertId()
	return id
}

func TestScheduledAccountAvailabilityMatchesAccountEnabledState(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	seedScheduledMaterial(t, store, 131)
	if !srv.scheduledAccountAvailable(context.Background(), 1, "scheduled-account") {
		t.Fatal("enabled owned account should be available")
	}
	if srv.scheduledAccountAvailable(context.Background(), 2, "scheduled-account") {
		t.Fatal("account owned by another user should not be available")
	}
	if err := store.Cookies.SetStatus(context.Background(), "scheduled-account", false); err != nil {
		t.Fatal(err)
	}
	if srv.scheduledAccountAvailable(context.Background(), 1, "scheduled-account") {
		t.Fatal("disabled account should not be available")
	}
}

func TestScheduledPublishProfitMustBeStrictlyGreaterThanThirtyCents(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	id := seedScheduledMaterial(t, store, 130)
	check := srv.checkScheduledMaterial(context.Background(), 1, id, 30)
	if check.OK || !strings.Contains(strings.Join(check.Reasons, " "), "必须严格大于") {
		t.Fatalf("30-cent profit should fail: %+v", check)
	}
	if _, err := store.DB.Exec(`UPDATE product_materials SET skus_json=REPLACE(skus_json,'"price_cent":130','"price_cent":131') WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	check = srv.checkScheduledMaterial(context.Background(), 1, id, 30)
	if !check.OK || check.MinimumProfit != 31 {
		t.Fatalf("31-cent profit should pass: %+v", check)
	}
}

func TestScheduledPublishPreflightAcceptsLegacyNullVideos(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	id := seedScheduledMaterial(t, store, 131)
	if _, err := store.DB.Exec(`UPDATE product_materials SET videos_json='null',video_enabled=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	check := srv.checkScheduledMaterial(context.Background(), 1, id, 30)
	if !check.OK {
		t.Fatalf("legacy null videos should be treated as empty: %+v", check)
	}
}

func TestCreateScheduledPublishBatchStoresFiveToTenMinuteIntervals(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	first := seedScheduledMaterial(t, store, 131)
	var raw string
	if err := store.DB.QueryRow(`SELECT skus_json FROM product_materials WHERE id=?`, first).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	result, err := store.DB.Exec(`INSERT INTO product_materials(user_id,source_type,source_id,title,description,images_json,category_json,skus_json,status,created_at,updated_at,revision) VALUES(1,'pdd','123','第二素材','描述','["https://img.pddpic.com/1.jpg"]','{}',?,'draft',1,1,1)`, raw)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := result.LastInsertId()
	payload := `{"material_ids":[` + strconv.FormatInt(first, 10) + `,` + strconv.FormatInt(second, 10) + `],"account_id":"scheduled-account","minimum_profit_cent":30,"start_at":` + strconv.FormatInt(time.Now().Unix()+60, 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/scheduled-publish/batches", strings.NewReader(payload))
	req = req.WithContext(auth.WithSession(req.Context(), &db.Session{UserID: 1}))
	rec := httptest.NewRecorder()
	srv.createScheduledPublishBatch(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var interval int64
	if err := store.DB.QueryRow(`SELECT interval_seconds FROM scheduled_publish_tasks WHERE material_id=?`, second).Scan(&interval); err != nil {
		t.Fatal(err)
	}
	if interval < 300 || interval > 600 {
		t.Fatalf("interval=%d, want 300..600", interval)
	}
}

func TestFailedScheduledTaskCannotBeClaimedAgain(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	id := seedScheduledMaterial(t, store, 131)
	batchID, taskID := "batch", "task"
	_, _ = store.DB.Exec(`INSERT INTO scheduled_publish_batches(id,user_id,account_id,start_at,task_count,status,created_at) VALUES(?,1,'scheduled-account',1,1,'running',1)`, batchID)
	_, _ = store.DB.Exec(`INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,not_before,status,attempt_count,idempotency_key,snapshot_json,created_at) VALUES(?,?,1,?,1,'scheduled-account',1,1,1,'failed',1,'once','{}',1)`, taskID, batchID, id)
	srv.executeScheduledPublishTask(context.Background(), taskID)
	var status string
	var attempts int
	if err := store.DB.QueryRow(`SELECT status,attempt_count FROM scheduled_publish_tasks WHERE id=?`, taskID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 1 {
		t.Fatalf("failed task changed: status=%s attempts=%d", status, attempts)
	}
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", taskID)
	payload := `{"start_at":` + strconv.FormatInt(time.Now().Unix()+60, 10) + `}`
	req := httptest.NewRequest(http.MethodPost, "/scheduled-publish/tasks/task/clone", strings.NewReader(payload))
	req = req.WithContext(auth.WithSession(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext), &db.Session{UserID: 1}))
	rec := httptest.NewRecorder()
	srv.cloneScheduledPublishTask(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("clone status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cloned int
	_ = store.DB.QueryRow(`SELECT COUNT(*) FROM scheduled_publish_tasks WHERE material_id=? AND status='pending' AND id<>?`, id, taskID).Scan(&cloned)
	if cloned != 1 {
		t.Fatalf("cloned pending tasks=%d, want 1", cloned)
	}
}

func TestScheduledAccountFailurePausesAllPendingTasks(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	materialID := seedScheduledMaterial(t, store, 131)
	for _, batchID := range []string{"batch-a", "batch-b"} {
		if _, err := store.DB.Exec(`INSERT INTO scheduled_publish_batches(id,user_id,account_id,start_at,task_count,status,created_at) VALUES(?,1,'scheduled-account',1,1,'running',1)`, batchID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB.Exec(`INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,not_before,status,attempt_count,idempotency_key,snapshot_json,created_at) VALUES(?,?,1,?,1,'scheduled-account',1,1,1,'pending',0,?,'{}',1)`, "task-"+batchID, batchID, materialID, "key-"+batchID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB.Exec(`UPDATE scheduled_publish_tasks SET status='publishing',attempt_count=1 WHERE id='task-batch-a'`); err != nil {
		t.Fatal(err)
	}
	srv.failScheduledTask(context.Background(), "task-batch-a", "batch-a", "scheduled-account", "publish", "UNAUTHORIZED", "消息凭证被拒绝，请重新登录", true)
	var failedStatus, blockedStatus, batchStatus string
	_ = store.DB.QueryRow(`SELECT status FROM scheduled_publish_tasks WHERE id='task-batch-a'`).Scan(&failedStatus)
	_ = store.DB.QueryRow(`SELECT status FROM scheduled_publish_tasks WHERE id='task-batch-b'`).Scan(&blockedStatus)
	_ = store.DB.QueryRow(`SELECT status FROM scheduled_publish_batches WHERE id='batch-b'`).Scan(&batchStatus)
	if failedStatus != "failed" || blockedStatus != "blocked" || batchStatus != "paused" {
		t.Fatalf("unexpected states: failed=%s blocked=%s batch=%s", failedStatus, blockedStatus, batchStatus)
	}
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", "scheduled-account")
	req := httptest.NewRequest(http.MethodPost, "/scheduled-publish/accounts/scheduled-account/resume", strings.NewReader(`{}`))
	req = req.WithContext(auth.WithSession(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext), &db.Session{UserID: 1}))
	rec := httptest.NewRecorder()
	srv.resumeScheduledPublishAccount(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = store.DB.QueryRow(`SELECT status FROM scheduled_publish_tasks WHERE id='task-batch-b'`).Scan(&blockedStatus)
	if blockedStatus != "pending" {
		t.Fatalf("resumed task status=%s, want pending", blockedStatus)
	}
}

func TestScheduledTaskUsesActualFinishTimeForNextInterval(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	materialID := seedScheduledMaterial(t, store, 131)
	_, _ = store.DB.Exec(`INSERT INTO scheduled_publish_batches(id,user_id,account_id,start_at,task_count,status,created_at) VALUES('batch',1,'scheduled-account',1,2,'running',1)`)
	_, _ = store.DB.Exec(`INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,interval_seconds,not_before,status,attempt_count,idempotency_key,snapshot_json,created_at) VALUES('first','batch',1,?,1,'scheduled-account',1,1,0,1,'success',1,'key-first','{}',1)`, materialID)
	_, _ = store.DB.Exec(`INSERT INTO scheduled_publish_tasks(id,batch_id,user_id,material_id,material_revision,account_id,sequence_no,planned_at,interval_seconds,not_before,status,attempt_count,idempotency_key,snapshot_json,created_at) VALUES('second','batch',1,?,1,'scheduled-account',2,2,480,2,'pending',0,'key-second','{}',1)`, materialID)
	srv.delayNextScheduledTask(context.Background(), "batch", 1, 1000)
	var notBefore int64
	if err := store.DB.QueryRow(`SELECT not_before FROM scheduled_publish_tasks WHERE id='second'`).Scan(&notBefore); err != nil {
		t.Fatal(err)
	}
	if notBefore != 1480 {
		t.Fatalf("not_before=%d, want 1480", notBefore)
	}
}
