package app

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/testutil"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func TestAppCloseClosesDatabase(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	sqlDB, err := app.DB.DB()
	if err != nil {
		t.Fatalf("load sql db: %v", err)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	if err := sqlDB.Ping(); err == nil {
		t.Fatal("expected database ping to fail after app close")
	}
}

func TestNewWithConfigBuildsQuotaAutoRefreshWhenEnabled(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.QuotaAutoRefresh == nil {
		t.Fatal("expected quota auto refresh runner when enabled")
	}
	if app.QuotaService == nil {
		t.Fatal("expected quota service to remain available for manual refresh")
	}
}

func TestNewWithConfigSkipsQuotaAutoRefreshWhenDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.QuotaAutoRefreshEnabled = false
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.QuotaAutoRefresh != nil {
		t.Fatal("expected quota auto refresh runner to be skipped when disabled")
	}
	if app.QuotaService == nil {
		t.Fatal("expected quota service to remain available for manual refresh when auto refresh is disabled")
	}
}

func TestQuotaActiveRecorderIsDisabledWithAutoRefresh(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.QuotaAutoRefreshEnabled = false
	if recorder := quotaActiveRecorder(cfg, nil); recorder != nil {
		t.Fatalf("expected disabled quota auto refresh to avoid active recorder, got %T", recorder)
	}
}

func TestAppCloseWaitsForQuotaRefreshTasksBeforeDatabaseClose(t *testing.T) {
	waitCalled := make(chan struct{}, 1)
	quotaService := &quotaContextRecorder{contextSet: make(chan context.Context, 1), waitCalled: waitCalled}
	app := &App{QuotaService: quotaService}

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	select {
	case <-waitCalled:
	case <-time.After(time.Second):
		t.Fatal("expected App.Close to wait for quota refresh goroutines")
	}
}

func TestAppCloseStopsRealQuotaRefreshTasksBeforeDatabaseClose(t *testing.T) {
	db := testutil.OpenTestDatabase(t)
	if err := db.Create(&entities.UsageIdentity{Identity: "auth-1", Name: "auth-1", Provider: "claude", Type: "auth-file", AuthType: entities.UsageIdentityAuthTypeAuthFile}).Error; err != nil {
		t.Fatalf("seed usage identity returned error: %v", err)
	}
	block := make(chan struct{})
	handler := &appQuotaHandlerStub{block: block}
	quotaService := quota.NewServiceWithRegistry(db, quota.NewProviderRegistry(map[string]quota.ProviderHandler{"claude": handler}))
	quotaService.SetRefreshContext(context.Background())
	app := &App{DB: db, QuotaService: quotaService}

	response, err := quotaService.Refresh(context.Background(), quota.RefreshRequest{AuthIndexes: []string{"auth-1"}, Source: quota.RefreshSourceManual})
	if err != nil {
		t.Fatalf("Refresh returned error: %v", err)
	}
	waitForAppQuotaTaskStatus(t, quotaService, response.Tasks[0].AuthIndex, quota.RefreshTaskStatusRunning)

	if err := app.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	task, err := quotaService.GetRefreshTaskByAuthIndex(context.Background(), response.Tasks[0].AuthIndex)
	if err != nil {
		t.Fatalf("GetRefreshTaskByAuthIndex returned error after close: %v", err)
	}
	if task.Status != quota.RefreshTaskStatusFailed {
		t.Fatalf("expected app close to cancel and drain real quota worker before closing DB, got %+v", task)
	}
	if handler.callCount() != 0 {
		t.Fatalf("expected canceled quota worker not to complete provider call, got %d calls", handler.callCount())
	}
}

func TestNewWithConfigBuildsRedisIngestAndRouter(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected poller status provider to be initialized")
	}
	if app.RedisIngest == nil {
		t.Fatal("expected redis ingest runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected redis process runner to be initialized")
	}
	if app.Router == nil {
		t.Fatal("expected router to be initialized")
	}
	if app.LogCloser == nil {
		t.Fatal("expected log closer to be initialized")
	}
	if app.MetadataSync == nil {
		t.Fatal("expected metadata sync runner to be initialized")
	}
}

func TestNewWithConfigWiresMetadataRefreshControl(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	runner, ok := app.RedisIngest.(*poller.RedisIngestRunner)
	if !ok {
		t.Fatalf("expected redis ingest runner, got %T", app.RedisIngest)
	}
	runnerValue := reflect.ValueOf(runner).Elem()
	writer := runnerValue.FieldByName("writer")
	if writer.IsNil() {
		t.Fatal("expected redis ingest writer")
	}
	if got := writer.Elem().Type().String(); got != "*poller.ControlAwareRedisInboxWriter" {
		t.Fatalf("expected control-aware redis inbox writer, got %s", got)
	}
	observer := runnerValue.FieldByName("controlObserver")
	if observer.IsNil() {
		t.Fatal("expected metadata sync runner to observe redis ingest control messages")
	}
	if got := observer.Elem().Type().String(); got != "*app.MetadataSyncRunner" {
		t.Fatalf("expected metadata sync runner observer, got %s", got)
	}
	metadataSyncPtr := reflect.ValueOf(app.MetadataSync).Pointer()
	if got := observer.Elem().Pointer(); got != metadataSyncPtr {
		t.Fatalf("expected redis ingest observer to share app metadata sync runner, got %x want %x", got, metadataSyncPtr)
	}
	writerObserver := writer.Elem().Elem().FieldByName("observer")
	if writerObserver.IsNil() {
		t.Fatal("expected redis inbox writer observer")
	}
	if got := writerObserver.Elem().Type().String(); got != "*app.MetadataSyncRunner" {
		t.Fatalf("expected redis inbox writer metadata sync observer, got %s", got)
	}
	if got := writerObserver.Elem().Pointer(); got != metadataSyncPtr {
		t.Fatalf("expected redis inbox writer observer to share app metadata sync runner, got %x want %x", got, metadataSyncPtr)
	}
}

func TestNewWithConfigExposesConfiguredCPAPublicURL(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.CPAPublicURL = "https://cpa.public.example.com/"
	app, err := NewWithConfig(cfg)
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	app.Router.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.Code)
	}
	body := resp.Body.String()
	if !strings.Contains(body, `"cpa_public_url":"https://cpa.public.example.com/"`) {
		t.Fatalf("expected CPA public URL in status response, got %s", body)
	}
	if strings.Contains(body, "cpa_management_url") {
		t.Fatalf("expected status response to use cpa_public_url instead of cpa_management_url, got %s", body)
	}
}

func TestNewWithConfigAggregatesExistingOverviewStatsBeforeRunnersStart(t *testing.T) {
	db := testutil.OpenTestDatabase(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{
		{EventKey: "legacy-event", APIGroupKey: "provider-a", Model: "claude-sonnet", Timestamp: time.Date(2026, 4, 16, 10, 10, 0, 0, time.UTC), TotalTokens: 150},
	}); err != nil {
		t.Fatalf("InsertUsageEvents returned error: %v", err)
	}

	logDir := t.TempDir()

	cfg := testAppConfig(t)
	cfg.LogFileEnabled = true
	cfg.LogDir = logDir
	app, err := NewWithConfigWithDB(cfg, db)
	if err != nil {
		t.Fatalf("NewWithConfigWithDB returned error: %v", err)
	}
	defer app.Close()

	var checkpoint entities.UsageOverviewAggregationCheckpoint
	if err := app.DB.Where("name = ?", "overview").First(&checkpoint).Error; err != nil {
		t.Fatalf("load overview checkpoint returned error: %v", err)
	}
	if checkpoint.LastAggregatedUsageEventID == 0 {
		t.Fatalf("expected startup catch-up to aggregate legacy usage events, got checkpoint %+v", checkpoint)
	}
	logContent := readAppLogFile(t, logDir)
	if !strings.Contains(logContent, "starting usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up start log, got %s", logContent)
	}
	if !strings.Contains(logContent, "completed usage overview aggregation catch-up") {
		t.Fatalf("expected startup catch-up completion log, got %s", logContent)
	}
}

func TestNewWithConfigSelectsRedisIngestRunners(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if _, ok := app.Poller.(*poller.RedisPoller); !ok {
		t.Fatalf("expected redis status provider to use redis poller, got %T", app.Poller)
	}
	if _, ok := app.RedisIngest.(*poller.RedisIngestRunner); !ok {
		t.Fatalf("expected redis ingest runner, got %T", app.RedisIngest)
	}
	if _, ok := app.RedisProcess.(*poller.RedisProcessRunner); !ok {
		t.Fatalf("expected redis process runner, got %T", app.RedisProcess)
	}
	if app.Maintenance == nil {
		t.Fatal("expected maintenance cleanup runner to be initialized")
	}
}

func TestNewWithConfigCreatesIndependentMaintenanceRunner(t *testing.T) {
	app, err := NewWithConfig(testAppConfig(t))
	if err != nil {
		t.Fatalf("NewWithConfig returned error: %v", err)
	}
	defer app.Close()
	if app.Poller == nil {
		t.Fatal("expected sync status provider to be initialized")
	}
	if app.RedisIngest == nil {
		t.Fatal("expected independent redis ingest runner to be initialized")
	}
	if app.RedisProcess == nil {
		t.Fatal("expected independent redis process runner to be initialized")
	}
	if app.Maintenance == nil {
		t.Fatal("expected independent maintenance runner to be initialized")
	}
}

func TestRunStartsPollerAndMaintenanceIndependently(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	pullStarted := make(chan struct{})
	processStarted := make(chan struct{})
	maintenanceStarted := make(chan struct{})
	metadataStarted := make(chan struct{}, 1)
	maintenance := NewStorageCleanupRunner(&maintenanceSyncStub{})
	maintenance.sleep = func(context.Context, time.Duration) bool {
		close(maintenanceStarted)
		return false
	}
	metadataRunner := NewMetadataSyncRunner(&metadataSyncStub{}, time.Second)
	metadataRunner.onStart = func() {
		select {
		case metadataStarted <- struct{}{}:
		default:
		}
	}
	statusProvider := &appRunStub{started: make(chan struct{})}
	app := &App{
		Config:       &cfg,
		Router:       gin.New(),
		Poller:       statusProvider,
		RedisIngest:  &appRunStub{started: pullStarted},
		RedisProcess: &appRunStub{started: processStarted},
		Maintenance:  maintenance,
		MetadataSync: metadataRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-pullStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis ingest runner to start")
	}
	select {
	case <-processStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis process runner to start")
	}
	select {
	case <-statusProvider.started:
		t.Fatal("expected poller status provider not to be started as a background runner")
	default:
	}
	select {
	case <-maintenanceStarted:
	case <-time.After(time.Second):
		t.Fatal("expected maintenance runner to start")
	}
	select {
	case <-metadataStarted:
	case <-time.After(time.Second):
		t.Fatal("expected metadata sync runner to start")
	}
}
func TestRunSetsQuotaServiceContextEvenWhenAutoRefreshDisabled(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	cfg.QuotaAutoRefreshEnabled = false
	quotaService := &quotaContextRecorder{contextSet: make(chan context.Context, 1)}
	app := &App{
		Config:       &cfg,
		Router:       gin.New(),
		QuotaService: quotaService,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case ctx := <-quotaService.contextSet:
		if ctx == nil {
			t.Fatal("expected quota service context to be non-nil")
		}
	case <-time.After(time.Second):
		t.Fatal("expected quota service context to be set")
	}
}

func TestRunCancelsBackgroundTasksWhenRouterStops(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = "invalid-port"
	ingestStarted := make(chan struct{})
	ingestCanceled := make(chan struct{})
	ingestRunner := &appCancelRecorder{started: ingestStarted, canceled: ingestCanceled}
	app := &App{
		Config:      &cfg,
		Router:      gin.New(),
		RedisIngest: ingestRunner,
	}

	if err := app.Run(); err == nil {
		t.Fatal("expected Run to return an error for invalid port")
	}
	select {
	case <-ingestStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis ingest runner to start")
	}
	select {
	case <-ingestCanceled:
	case <-time.After(time.Second):
		t.Fatal("expected redis ingest runner context to be canceled")
	}
}

type quotaContextRecorder struct {
	contextSet chan context.Context
	waitCalled chan struct{}
}

func (r *quotaContextRecorder) SetRefreshContext(ctx context.Context) {
	r.contextSet <- ctx
}

func (r *quotaContextRecorder) StartAutoRefresh(context.Context) error {
	return nil
}

func (r *quotaContextRecorder) WaitRefreshTasks() {
	if r.waitCalled != nil {
		r.waitCalled <- struct{}{}
	}
}

func (r *quotaContextRecorder) StopRefreshTasks() {
	r.WaitRefreshTasks()
}

type appQuotaHandlerStub struct {
	block <-chan struct{}
	calls int
}

func (s *appQuotaHandlerStub) Check(ctx context.Context, input quota.ProviderInput) (quota.ProviderOutput, error) {
	select {
	case <-ctx.Done():
		return quota.ProviderOutput{}, ctx.Err()
	case <-s.block:
	}
	s.calls++
	return quota.ProviderOutput{Result: quota.ClaudeResult{Usage: &quota.ClaudeUsagePayload{FiveHour: &quota.ClaudeUsageWindow{Utilization: 25}}}}, nil
}

func (s *appQuotaHandlerStub) callCount() int {
	return s.calls
}

func waitForAppQuotaTaskStatus(t *testing.T, service *quota.Service, authIndex string, status quota.RefreshTaskStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var task quota.RefreshTaskResponse
	var err error
	for time.Now().Before(deadline) {
		task, err = service.GetRefreshTaskByAuthIndex(context.Background(), authIndex)
		if err == nil && task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("auth_index %s did not reach status %s, last task=%+v err=%v", authIndex, status, task, err)
}

type appRunStub struct {
	started chan struct{}
}

type appCancelRecorder struct {
	started  chan struct{}
	canceled chan struct{}
}

func (r *appCancelRecorder) Run(ctx context.Context) error {
	close(r.started)
	<-ctx.Done()
	close(r.canceled)
	return nil
}

func (s *appRunStub) Run(context.Context) error {
	close(s.started)
	return nil
}

func (s *appRunStub) Status() poller.Status {
	return poller.Status{}
}

func captureAppInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousOutput := logrus.StandardLogger().Out
	previousFormatter := logrus.StandardLogger().Formatter
	previousLevel := logrus.GetLevel()
	logrus.SetOutput(&logs)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	logrus.SetLevel(logrus.InfoLevel)
	t.Cleanup(func() {
		logrus.SetOutput(previousOutput)
		logrus.SetFormatter(previousFormatter)
		logrus.SetLevel(previousLevel)
	})
	return &logs
}

func readAppLogFile(t *testing.T, logDir string) string {
	t.Helper()
	path := filepath.Join(logDir, "cpa-usage-keeper-"+time.Now().Format("2006-01-02")+".log")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read app log file: %v", err)
	}
	return string(content)
}

func testAppConfig(t *testing.T) config.Config {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://test:test@localhost:5432/test?sslmode=disable"
	}
	return config.Config{
		AppPort:                 "8080",
		CPABaseURL:              "https://cpa.example.com",
		CPAManagementKey:        "secret",
		RedisQueueIdleInterval:  time.Second,
		MetadataSyncInterval:    30 * time.Second,
		DatabaseURL:             databaseURL,
		RequestTimeout:          5 * time.Second,
		QuotaAutoRefreshEnabled: true,
		LogLevel:                "info",
		LogFileEnabled:          false,
		LogRetentionDays:        7,
	}
}

func TestBuildHTTPServerAppliesConfiguredTimeouts(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.HTTPReadHeaderTimeout = 11 * time.Second
	cfg.HTTPReadTimeout = 21 * time.Second
	cfg.HTTPWriteTimeout = 31 * time.Second
	cfg.HTTPIdleTimeout = 41 * time.Second
	app := &App{Config: &cfg, Router: gin.New()}

	server := app.buildHTTPServer()

	if server.Addr != ":"+cfg.AppPort {
		t.Fatalf("expected addr :%s, got %s", cfg.AppPort, server.Addr)
	}
	if server.ReadHeaderTimeout != 11*time.Second {
		t.Fatalf("expected read header timeout 11s, got %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 21*time.Second {
		t.Fatalf("expected read timeout 21s, got %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 31*time.Second {
		t.Fatalf("expected write timeout 31s, got %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 41*time.Second {
		t.Fatalf("expected idle timeout 41s, got %s", server.IdleTimeout)
	}
}

func TestRunGracefulShutdownOnSignal(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = freePort(t)
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.HTTPReadHeaderTimeout = 5 * time.Second
	cfg.HTTPReadTimeout = 5 * time.Second
	cfg.HTTPWriteTimeout = 5 * time.Second
	cfg.HTTPIdleTimeout = 5 * time.Second

	sigCh := make(chan os.Signal, 1)
	app := &App{Config: &cfg, Router: gin.New(), shutdownSignal: sigCh}

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	waitForServerUp(t, net.JoinHostPort("127.0.0.1", cfg.AppPort), 2*time.Second)
	sigCh <- os.Interrupt

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected Run to return nil on graceful shutdown, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected Run to return after shutdown signal")
	}
}

func TestRunCancelsBackgroundBeforeReturningOnSignal(t *testing.T) {
	cfg := testAppConfig(t)
	cfg.AppPort = freePort(t)
	cfg.ShutdownTimeout = 2 * time.Second
	cfg.HTTPReadHeaderTimeout = 5 * time.Second
	cfg.HTTPReadTimeout = 5 * time.Second
	cfg.HTTPWriteTimeout = 5 * time.Second
	cfg.HTTPIdleTimeout = 5 * time.Second

	ingestStarted := make(chan struct{})
	ingestCanceled := make(chan struct{})
	sigCh := make(chan os.Signal, 1)
	app := &App{
		Config:         &cfg,
		Router:         gin.New(),
		RedisIngest:    &appCancelRecorder{started: ingestStarted, canceled: ingestCanceled},
		shutdownSignal: sigCh,
	}

	done := make(chan error, 1)
	go func() { done <- app.Run() }()

	select {
	case <-ingestStarted:
	case <-time.After(time.Second):
		t.Fatal("expected redis ingest runner to start")
	}
	waitForServerUp(t, net.JoinHostPort("127.0.0.1", cfg.AppPort), 2*time.Second)
	sigCh <- os.Interrupt

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected Run to return nil on graceful shutdown, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expected Run to return after shutdown signal")
	}

	// stopBackgroundTasks 必须在 Run 返回前 Wait 完 runner，因此 canceled 此时已关闭。
	select {
	case <-ingestCanceled:
	default:
		t.Fatal("expected redis ingest runner context to be canceled before Run returned")
	}
}

// freePort 申请一个空闲 TCP 端口供测试绑定，返回端口号字符串。
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate free port: %v", err)
	}
	defer ln.Close()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	return port
}

// waitForServerUp 轮询 TCP 拨号直到端口可连，确认 HTTP 服务已开始监听。
func waitForServerUp(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not come up within %s", addr, timeout)
}
