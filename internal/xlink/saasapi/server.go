// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// saas-api 商户后台/客户端/访客服务（字典 API-TEN-xx / API-C-xx / API-G-xx）
// M2 首批：商户后台登录 + 客服成员管理（经 gRPC 调 OpenIM user rpc 注册）

package saasapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/license"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"google.golang.org/grpc"
)

type Server struct {
	cfg      *platform.Config
	store    *platform.Store
	userConn grpc.ClientConnInterface
	authConn grpc.ClientConnInterface
	rdb      redis.UniversalClient
	gate     *platform.LicenseGate
}

func NewServer(cfg *platform.Config, store *platform.Store, userConn, authConn grpc.ClientConnInterface, rdb redis.UniversalClient, gate *platform.LicenseGate) *Server {
	return &Server{cfg: cfg, store: store, userConn: userConn, authConn: authConn, rdb: rdb, gate: gate}
}

// licenseGuard 授权门禁（私有化降级时 45101/45102）
func (s *Server) licenseGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := s.gate.Check(); err != nil {
			apiresp.GinError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) licenseStatus(c *gin.Context) {
	resp := gin.H{"enabled": s.gate.Enabled()}
	ll, status, err := license.CurrentState(s.cfg.License.LocalDir)
	resp["status"] = status
	if err != nil {
		if cd, ok := errs.Unwrap(err).(errs.CodeError); ok {
			resp["code"] = cd.Code()
		}
		resp["reason"] = err.Error()
	}
	if ll != nil && ll.Payload != nil {
		resp["licenseNo"] = ll.Payload.LicenseID
		resp["merchant"] = ll.Payload.Merchant
		resp["mode"] = ll.Payload.Mode
		resp["notAfter"] = ll.Payload.NotAfter
	}
	apiresp.GinSuccess(c, resp)
}

func (s *Server) Router() http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) {
		if c.GetHeader("operationID") == "" {
			c.Request.Header.Set("operationID", fmt.Sprintf("saas-%d", timeNowUnixNano()))
		}
		c.Next()
	})

	r.GET("/license/status", s.licenseStatus)                          // 授权状态（激活向导依赖，任何状态可用）
	r.POST("/tenant/auth/login", s.licenseGuard(), s.login)            // API-TEN-01

	// 访客免鉴权区（限流+黑名单在 handler 内）API-G-01/02/03/04/05；降级时仅保留 config
	guest := r.Group("/g")
	{
		guest.GET("/config", s.guestConfig)
		blocked := guest.Group("", s.licenseGuard())
		{
			blocked.POST("/init", s.guestInit)
			blocked.POST("/renew", s.guestRenew)
			blocked.POST("/offline-message", s.guestOfflineMessage)
			blocked.POST("/rate", s.guestRate)
		}
	}
	// 客户端登录（客服四端凭据）API-C-01
	r.POST("/c/auth/login", s.licenseGuard(), s.clientLogin)

	// 坐席工作台（IM token 鉴权）API-C-04~07/10
	agent := r.Group("/c", s.imAuthMiddleware(), s.licenseGuard())
	{
		agent.POST("/agent/heartbeat", s.agentHeartbeat)
		agent.GET("/agent/queue", s.agentQueue)
		agent.POST("/agent/claim", s.agentClaim)
		agent.POST("/agent/transfer", s.agentTransfer)
		agent.POST("/agent/session/:id/close", s.agentCloseSession)
	}

	authed := r.Group("/tenant", s.authMiddleware(), s.licenseGuard())
	{
		// 成员管理 API-TEN-02/03
		authed.GET("/staffs", s.listStaffs)
		authed.POST("/staffs", s.createStaff)
		authed.PUT("/staffs/:id", s.updateStaff)
		// 访客接入/黑名单/组件代码/日汇总 API-TEN-07/08/09
		authed.GET("/guest-config", s.getGuestConfig)
		authed.PUT("/guest-config", s.putGuestConfig)
		authed.GET("/guest-blacklist", s.listBlacklist)
		authed.POST("/guest-blacklist", s.addBlacklist)
		authed.DELETE("/guest-blacklist/:id", s.removeBlacklist)
		authed.GET("/widget-code", s.getWidgetCode)
		authed.PUT("/widget-code", s.putWidgetCode)
		authed.GET("/stats/daily", s.statsDaily)
		// 接待记录 API-TEN-10/11
		authed.GET("/agent-sessions", s.listSessions)
		authed.GET("/agent-sessions/:id/messages", s.sessionMessages)
		// 配额 API-TEN-16
		authed.GET("/quota", s.quota)
	}
	r.GET("/healthz", func(c *gin.Context) { apiresp.GinSuccess(c, gin.H{"status": "ok"}) })
	return r
}

// authMiddleware 商户后台 JWT 鉴权（issuer=xlink:tenant），并把租户写入 ctx
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Request.Header.Get("token")
		if token == "" {
			token = bearerToken(c.Request.Header.Get("Authorization"))
		}
		if token == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("missing token"))
			c.Abort()
			return
		}
		claims, err := platform.ParseToken(token, s.cfg.JWT.TenantSecret, platform.IssuerTenant)
		if err != nil {
			apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("tenant token invalid"))
			c.Abort()
			return
		}
		// [XunLink] 租户上下文可见性（gin keys）：下游 gRPC 调用在 handler 内
		// 用 tenantctx.WithTenantID 显式构造 ctx 透传
		c.Set(tenantctx.TenantIDKey, fmt.Sprintf("%d", claims.TenantID))
		c.Set("tenant_id", claims.TenantID)
		c.Set("tenant_account", claims.Account)
		c.Set("tenant_role", claims.Role)
		c.Next()
	}
}

func bearerToken(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return h[7:]
	}
	return ""
}
