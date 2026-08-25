package api

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"cpa-usage-keeper/internal/auth"
	"cpa-usage-keeper/internal/logging"
	"cpa-usage-keeper/internal/poller"
	"cpa-usage-keeper/internal/quota"
	rankinghttpapi "cpa-usage-keeper/internal/ranking/httpapi"
	"cpa-usage-keeper/internal/service"
	"cpa-usage-keeper/internal/updatecheck"
	"cpa-usage-keeper/internal/version"
	"github.com/gin-gonic/gin"
)

const appBasePathPlaceholder = "__APP_BASE_PATH__"

var loopbackTrustedProxyCIDRs = []string{"127.0.0.1/32", "::1/128"}

type StatusProvider interface {
	Status() poller.Status
}

type ActiveStatusRecorder interface {
	RecordActiveStatus(time.Time)
}

type QuotaProvider interface {
	GetCodexQuotaHistory(context.Context, quota.CodexQuotaHistoryRequest) (quota.CodexQuotaHistoryResponse, error)
	GetCachedQuota(context.Context, quota.CacheRequest) (quota.CacheResponse, error)
	Refresh(context.Context, quota.RefreshRequest) (quota.RefreshResponse, error)
	GetRefreshTaskByAuthIndex(context.Context, string) (quota.RefreshTaskResponse, error)
	GetInspectionStatus(context.Context) (quota.InspectionStatus, error)
	StartInspection(context.Context) (quota.InspectionStatus, error)
	GetAutoRefreshSettings(context.Context) (quota.AutoRefreshSettings, error)
	UpdateAutoRefreshSettings(context.Context, quota.AutoRefreshSettings) (quota.AutoRefreshSettings, error)
	GetResetCredits(context.Context, quota.ResetCreditsRequest) (quota.ResetCreditsResponse, error)
	Reset(context.Context, quota.ResetRequest) (quota.ResetResponse, error)
}

type StatusRouteConfig struct {
	CPAPublicURL               string
	CPARequestLogAccessEnabled bool
	ActiveRecorder             ActiveStatusRecorder
	QuotaAutoRefreshEnabled    bool
}

type OptionalProviders struct {
	UsageIdentity service.UsageIdentityProvider
	ErrorEvents   service.ErrorEventProvider
	Quota         QuotaProvider
	CPAAPIKeys    service.CPAAPIKeyProvider
	AuthFiles     service.AuthFilesManagementProvider
	RequestLogs   service.RequestLogProvider
	Ranking       rankinghttpapi.Provider
	LocalRanking  rankinghttpapi.LocalProvider
	Status        StatusRouteConfig
}

func NewRouter(
	staticFS fs.FS,
	statusProvider StatusProvider,
	usageProvider service.UsageProvider,
	pricingProvider service.PricingProvider,
	authConfig AuthConfig,
	authHandler *authHandler,
	basePath string,
	optionalProviders ...OptionalProviders,
) *gin.Engine {
	router := gin.New()
	trustedProxyCIDRs := append([]string{}, loopbackTrustedProxyCIDRs...)
	trustedProxyCIDRs = append(trustedProxyCIDRs, authConfig.TrustedProxyCIDRs...)
	_ = router.SetTrustedProxies(trustedProxyCIDRs)
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	router.Use(logging.NewGinRecovery())

	appGroup := router.Group(basePath)
	registerHealthRoutes(appGroup)

	apiV1 := appGroup.Group("/api/v1")
	// 写请求（POST/PUT/PATCH/DELETE）必须携带 request-intent header，防止跨站表单/链接伪造。
	apiV1.Use(unauthenticatedLoginRequestLimits(basePath))
	apiV1.Use(requestIntentMiddleware())
	if debugAPIRoutesEnabled() {
		registerPingRoutes(apiV1)
	}

	authGroup := apiV1.Group("/auth")
	if authHandler == nil {
		authHandler = NewAuthHandler(authConfig, nil)
	}
	authHandler.registerRoutes(authGroup)

	var usageIdentityProvider service.UsageIdentityProvider
	var errorEventProvider service.ErrorEventProvider
	var quotaProvider QuotaProvider
	var cpaAPIKeyProvider service.CPAAPIKeyProvider
	var authFilesProvider service.AuthFilesManagementProvider
	var requestLogProvider service.RequestLogProvider
	var rankingProvider rankinghttpapi.Provider
	var localRankingProvider rankinghttpapi.LocalProvider
	var statusConfig StatusRouteConfig
	if len(optionalProviders) > 0 {
		usageIdentityProvider = optionalProviders[0].UsageIdentity
		errorEventProvider = optionalProviders[0].ErrorEvents
		quotaProvider = optionalProviders[0].Quota
		cpaAPIKeyProvider = optionalProviders[0].CPAAPIKeys
		authFilesProvider = optionalProviders[0].AuthFiles
		requestLogProvider = optionalProviders[0].RequestLogs
		rankingProvider = optionalProviders[0].Ranking
		localRankingProvider = optionalProviders[0].LocalRanking
		statusConfig = optionalProviders[0].Status
	}
	requestLogDownloadTokens := newRequestLogDownloadTokenStore()

	registerUsageEventRequestLogDownloadTokenRoutes(apiV1, requestLogProvider, requestLogDownloadTokens, statusConfig.CPARequestLogAccessEnabled)
	authHandler.setCPAAPIKeyProvider(cpaAPIKeyProvider)

	versionProtected := apiV1.Group("")
	versionProtected.Use(authHandler.roleMiddleware(auth.RoleAdmin, auth.RoleAPIKeyViewer))
	registerVersionRoutes(versionProtected)

	adminProtected := apiV1.Group("")
	adminProtected.Use(authHandler.adminMiddleware())
	registerStatusRoutes(adminProtected, statusProvider, statusConfig)
	registerUpdateRoutes(adminProtected, nil)
	registerUsageModelsRoute(adminProtected, usageProvider)
	registerUsageOverviewRoute(adminProtected, usageProvider, cpaAPIKeyProvider)
	registerUsageActivityRoute(adminProtected, usageProvider)
	registerUsageAnalysisRoute(adminProtected, usageProvider, cpaAPIKeyProvider)
	registerUsageEventsRoute(adminProtected, usageProvider, usageIdentityProvider, cpaAPIKeyProvider, requestLogProvider, requestLogDownloadTokens, statusConfig.CPARequestLogAccessEnabled)
	registerUsageIdentityRoutes(adminProtected, usageIdentityProvider)
	registerErrorEventRoutes(adminProtected, errorEventProvider)
	registerAuthFileManagementRoutes(adminProtected, authFilesProvider)
	registerAuthSessionManagementRoutes(adminProtected, authHandler)
	registerCPAAPIKeyRoutes(adminProtected, cpaAPIKeyProvider)
	registerPricingRoutes(adminProtected, pricingProvider)
	registerQuotaRoutes(adminProtected, quotaProvider)
	if rankingProvider != nil {
		rankinghttpapi.RegisterRoutes(adminProtected, rankingProvider)
	}
	if localRankingProvider != nil {
		rankinghttpapi.RegisterLocalRoutes(adminProtected, localRankingProvider)
	}

	keyViewerProtected := apiV1.Group("")
	keyViewerProtected.Use(authHandler.apiKeyViewerMiddleware())
	registerKeyOverviewRoute(keyViewerProtected, usageProvider, cpaAPIKeyProvider, authHandler)
	registerKeyActivityRoute(keyViewerProtected, usageProvider, cpaAPIKeyProvider, authHandler)

	if staticFS != nil {
		if indexFile, err := staticFS.Open("index.html"); err == nil {
			_ = indexFile.Close()
			httpFS := http.FS(staticFS)
			serveIndex := func(c *gin.Context) {
				indexHTML, err := renderIndexHTML(staticFS, basePath)
				if err != nil {
					c.Status(http.StatusNotFound)
					return
				}
				setHTMLCacheHeaders(c)
				setFrameAncestorsCSP(c, authConfig.FrameAncestorOrigins)
				c.Data(http.StatusOK, "text/html; charset=utf-8", indexHTML)
			}
			serveAsset := func(c *gin.Context) {
				assetPath := "assets/" + strings.TrimPrefix(c.Param("filepath"), "/")
				if assetFile, err := staticFS.Open(assetPath); err == nil {
					_ = assetFile.Close()
					setStaticAssetCacheHeaders(c)
					c.FileFromFS(assetPath, httpFS)
					return
				}
				c.Status(http.StatusNotFound)
			}

			appGroup.GET("/", serveIndex)
			appGroup.GET("/assets/*filepath", serveAsset)
			appGroup.HEAD("/assets/*filepath", serveAsset)
			router.NoRoute(func(c *gin.Context) {
				requestPath, ok := stripBasePath(basePath, c.Request.URL.Path)
				if !ok {
					c.Status(http.StatusNotFound)
					return
				}
				if strings.HasPrefix(requestPath, "/api/") {
					c.Status(http.StatusNotFound)
					return
				}

				if assetPath, ok := staticAssetPath(requestPath); ok {
					if assetFile, err := staticFS.Open(assetPath); err == nil {
						_ = assetFile.Close()
						setStaticAssetCacheHeaders(c)
						c.FileFromFS(assetPath, httpFS)
						return
					}
				}

				serveIndex(c)
			})
		}
	}

	return router
}

func setHTMLCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// setNoStoreHeaders 只设置 no-store 缓存头，不附加 frame-ancestors CSP。
// 用于 version JSON 等非 HTML 响应——它们不是嵌入目标，不需要 CSP 防护。
func setNoStoreHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
}

// setFrameAncestorsCSP 为 HTML 响应设置 frame-ancestors CSP。
// 'self' 始终包含；额外的 origin 由 AuthConfig.FrameAncestorOrigins 提供。
func setFrameAncestorsCSP(c *gin.Context, origins []string) {
	parts := []string{"'self'"}
	for _, o := range origins {
		o = strings.TrimSpace(o)
		if o != "" {
			parts = append(parts, o)
		}
	}
	c.Header("Content-Security-Policy", "frame-ancestors "+strings.Join(parts, " "))
}

func setStaticAssetCacheHeaders(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
}

func renderIndexHTML(staticFS fs.FS, basePath string) ([]byte, error) {
	indexFile, err := staticFS.Open("index.html")
	if err != nil {
		return nil, err
	}
	defer indexFile.Close()
	indexHTML, err := io.ReadAll(indexFile)
	if err != nil {
		return nil, err
	}

	return bytes.ReplaceAll(
		indexHTML,
		[]byte(strconv.Quote(appBasePathPlaceholder)),
		[]byte(strconv.Quote(basePath)),
	), nil
}

func cleanURLPath(requestPath string) string {
	cleaned := path.Clean(requestPath)
	if cleaned == "." {
		return "/"
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "/" + cleaned
	}
	return cleaned
}

func staticAssetPath(requestPath string) (string, bool) {
	cleaned := cleanURLPath(requestPath)
	if strings.Contains(cleaned, "\\") {
		return "", false
	}
	relPath := strings.TrimPrefix(cleaned, "/")
	if relPath == "" {
		return "", false
	}
	return relPath, true
}

func stripBasePath(basePath, requestPath string) (string, bool) {
	cleaned := cleanURLPath(requestPath)
	if basePath == "" {
		return cleaned, true
	}
	if cleaned == basePath {
		return "/", true
	}
	if !strings.HasPrefix(cleaned, basePath+"/") {
		return "", false
	}
	trimmed := strings.TrimPrefix(cleaned, basePath)
	if trimmed == "" {
		return "/", true
	}
	return trimmed, true
}

type statusResponse struct {
	Running                    bool       `json:"running"`
	SyncRunning                bool       `json:"sync_running"`
	Timezone                   string     `json:"timezone"`
	QuotaAutoRefreshEnabled    bool       `json:"quotaAutoRefreshEnabled"`
	CPAPublicURL               string     `json:"cpa_public_url,omitempty"`
	CPARequestLogAccessEnabled bool       `json:"cpa_request_log_access_enabled"`
	LastRunAt                  *time.Time `json:"last_run_at,omitempty"`
	LastError                  string     `json:"last_error,omitempty"`
	LastWarning                string     `json:"last_warning,omitempty"`
	LastStatus                 string     `json:"last_status,omitempty"`
}

type versionResponse struct {
	Version            string `json:"version"`
	UpdateCheckEnabled bool   `json:"updateCheckEnabled"`
}

func registerVersionRoutes(router gin.IRoutes) {
	router.GET("/version", func(c *gin.Context) {
		setNoStoreHeaders(c)
		c.JSON(http.StatusOK, buildVersionResponse())
	})
}

func buildVersionResponse() versionResponse {
	return versionResponse{
		Version:            version.Version,
		UpdateCheckEnabled: updatecheck.IsStableVersion(version.Version),
	}
}

func debugAPIRoutesEnabled() bool {
	return version.Version == "dev" || os.Getenv("GIN_MODE") == gin.DebugMode
}

func registerPingRoutes(router gin.IRoutes) {
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "ok"})
	})
}

func registerStatusRoutes(router gin.IRoutes, statusProvider StatusProvider, config StatusRouteConfig) {
	router.GET("/status", func(c *gin.Context) {
		if statusProvider == nil {
			c.JSON(http.StatusOK, buildStatusResponse(poller.Status{}, config))
			return
		}

		c.JSON(http.StatusOK, buildStatusResponse(statusProvider.Status(), config))
	})
	router.GET("/status/active", func(c *gin.Context) {
		if config.ActiveRecorder != nil {
			// 前端可见页面用这个轻量心跳续约，避免限额自动刷新在无人查看后台时持续扫库和请求上游。
			config.ActiveRecorder.RecordActiveStatus(time.Now())
		}
		c.Status(http.StatusNoContent)
	})
}

func buildStatusResponse(status poller.Status, config StatusRouteConfig) statusResponse {
	response := statusResponse{
		Running:                    status.Running,
		SyncRunning:                status.SyncRunning,
		Timezone:                   time.Local.String(),
		QuotaAutoRefreshEnabled:    config.QuotaAutoRefreshEnabled,
		CPAPublicURL:               config.CPAPublicURL,
		CPARequestLogAccessEnabled: config.CPARequestLogAccessEnabled,
		LastError:                  status.LastError,
		LastWarning:                status.LastWarning,
		LastStatus:                 status.LastStatus,
	}
	// fork 不向 /status 响应暴露 last_run_at（前端 Status 类型也刻意不含该字段）。
	return response
}
