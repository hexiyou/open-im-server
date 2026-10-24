// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// admin-api 总后台服务（API 前缀 /platform，字典 API-PLAT-xx）

package adminapi

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"google.golang.org/grpc"
)

type Server struct {
	cfg        *platform.Config
	store      *platform.Store
	authConn   grpc.ClientConnInterface // 租户停用时强制下线成员（auth rpc ForceLogout）
	privateKey *rsa.PrivateKey          // 激活码签发私钥（SaaS 平台侧；私有化实例为 nil）
	gate       *platform.LicenseGate    // 授权门禁（私有化实例启用）
}

func NewServer(cfg *platform.Config, store *platform.Store, authConn grpc.ClientConnInterface, privateKey *rsa.PrivateKey, gate *platform.LicenseGate) *Server {
	return &Server{cfg: cfg, store: store, authConn: authConn, privateKey: privateKey, gate: gate}
}

// SeedSuperAdmin 首次启动时初始化超级管理员（仅当账号表为空）
func (s *Server) SeedSuperAdmin(ctx context.Context) error {
	n, err := s.store.CountPlatformAdmins(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	hash, err := hashPassword(s.cfg.Seed.SuperPassword)
	if err != nil {
		return err
	}
	admin := &platform.PlatformAdmin{
		Username:     s.cfg.Seed.SuperUser,
		PasswordHash: hash,
		Role:         "super",
		Status:       1,
	}
	if err := s.store.CreatePlatformAdmin(ctx, admin); err != nil {
		return err
	}
	fmt.Printf("[xlink-admin-api] seeded super admin: %s (开发种子账号，生产环境务必修改密码)\n", s.cfg.Seed.SuperUser)
	return nil
}

func (s *Server) Router() http.Handler {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(func(c *gin.Context) { // operationID 兜底
		if c.GetHeader("operationID") == "" {
			c.Request.Header.Set("operationID", fmt.Sprintf("plat-%d", timeNowUnixNano()))
		}
		c.Next()
	})

	r.POST("/platform/auth/login", s.login) // API-PLAT-01

	// 授权状态查询与内部心跳（任何状态下可用，激活向导依赖）
	r.GET("/license/status", s.licenseStatus)
	r.POST("/internal/license/heartbeat", s.licenseHeartbeat)
	r.POST("/internal/license/activate-report", s.licenseActivateReport)

	authed := r.Group("/platform", s.authMiddleware(), s.licenseGuard())
	{
		// 租户管理 API-PLAT-03/04/05/06/07
		authed.GET("/tenants", s.listTenants)
		authed.POST("/tenants", s.writeGuard(), s.createTenant)
		authed.PUT("/tenants/:id", s.writeGuard(), s.updateTenant)
		authed.GET("/tenants/:id/detail", s.tenantDetail)
		authed.POST("/tenants/:id/reset-admin-password", s.writeGuard(), s.resetTenantAdminPassword)
		// 套餐管理 API-PLAT-08
		authed.GET("/plans", s.listPlans)
		authed.POST("/plans", s.writeGuard(), s.createPlan)
		// 授权管理 API-PLAT-09/10/11/12/17/18
		authed.GET("/licenses", s.listLicenses)
		authed.POST("/licenses/decode-apply", s.writeGuard(), s.decodeApply)
		authed.POST("/licenses/issue", s.writeGuard(), s.issueLicense)
		authed.POST("/licenses/:id/revoke", s.writeGuard(), s.revokeLicense)
		authed.POST("/licenses/:id/rebind", s.writeGuard(), s.rebindLicense)
		authed.POST("/licenses/:id/renew", s.writeGuard(), s.renewLicense)
		// 平台管理员管理 API-PLAT-16
		authed.GET("/admins", s.listAdmins)
		authed.POST("/admins", s.writeGuard(), s.createAdmin)
		authed.PUT("/admins/:id", s.writeGuard(), s.updateAdmin)
		// 审计 API-PLAT-15
		authed.GET("/audit-logs", s.listAuditLogs)
		// 工作台指标 API-PLAT-13
		authed.GET("/metrics/overview", s.metricsOverview)
	}
	r.GET("/healthz", func(c *gin.Context) { apiresp.GinSuccess(c, gin.H{"status": "ok"}) })
	return r
}

// authMiddleware 平台管理员 JWT 鉴权（issuer=xlink:platform）+ 授权门禁
func (s *Server) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// [XunLink] 授权门禁：私有化实例降级时锁定管理台（45101/45102）
		if _, err := s.gate.Check(); err != nil {
			apiresp.GinError(c, err)
			c.Abort()
			return
		}
		token := c.Request.Header.Get("token")
		if token == "" {
			token = bearerToken(c.Request.Header.Get("Authorization"))
		}
		if token == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("missing token"))
			c.Abort()
			return
		}
		claims, err := platform.ParseToken(token, s.cfg.JWT.PlatformSecret, platform.IssuerPlatform)
		if err != nil {
			apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("platform token invalid"))
			c.Abort()
			return
		}
		c.Set("plat_account", claims.Account)
		c.Set("plat_role", claims.Role)
		c.Next()
	}
}

// writeGuard 只读角色禁止写操作（错误码 40303）
func (s *Server) writeGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetString("plat_role") == "readonly" {
			apiresp.GinError(c, errs.NewCodeError(40303, "no permission").WrapMsg("readonly role"))
			c.Abort()
			return
		}
		c.Next()
	}
}

func bearerToken(h string) string {
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return h[7:]
	}
	return ""
}
