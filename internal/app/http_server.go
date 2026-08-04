package app

import (
	"net/http"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/logging"
	"github.com/sirupsen/logrus"
)

const (
	httpMaxHeaderBytes = 64 << 10
)

// NewHTTPServer 构造 HTTP 服务实例。
//
// ReadHeaderTimeout / IdleTimeout 走 fork 可配置字段(默认 5s / 60s);
// ReadTimeout / WriteTimeout 故意不设置(=0),避免截断已认证的长响应——
// 这对流式导出(CSV/JSON,见 /usage/events/export)至关重要。
// MaxHeaderBytes 固定 64KiB 限制请求头体积。
func NewHTTPServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           handler,
		ErrorLog:          logging.NewStandardLogger(logrus.ErrorLevel),
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
		MaxHeaderBytes:    httpMaxHeaderBytes,
	}
}
