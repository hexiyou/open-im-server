// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// saas-api 商户后台/客户端/访客服务（字典 API-TEN-xx / API-C-xx / API-G-xx）
// M2 首批：商户后台登录 + 客服成员管理（经 gRPC 调 OpenIM user rpc 注册）

package saasapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Server struct {
	cfg      *platform.Config
	store    *platform.Store
	userConn grpc.ClientConnInterface
}

func NewServer(cfg *platform.Config, store *platform.Store, userConn grpc.ClientConnInterface) *Server {
	return &Server{cfg: cfg, store: store, userConn: userConn}
}

// BuildUserConn 建立 OpenIM user rpc 连接（etcd 服务发现）
func BuildUserConn(ctx context.Context, cfg *platform.Config) (grpc.ClientConnInterface, error) {
	dis := &config.Discovery{
		Enable: "etcd",
		Etcd: config.Etcd{
			RootDirectory: cfg.OpenIM.EtcdRootDir,
			Address:       cfg.OpenIM.EtcdAddress,
		},
	}
	share := &config.Share{}
	share.RpcRegisterName.User = "user"
	client, err := kdisc.NewDiscoveryRegister(dis, share, nil)
	if err != nil {
		return nil, errs.WrapMsg(err, "discovery init failed")
	}
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"LoadBalancingPolicy": "round_robin"}`))
	conn, err := client.GetConn(ctx, share.RpcRegisterName.User)
	if err != nil {
		return nil, errs.WrapMsg(err, "connect user rpc failed")
	}
	return conn, nil
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

	r.POST("/tenant/auth/login", s.login) // API-TEN-01

	authed := r.Group("/tenant", s.authMiddleware())
	{
		// 成员管理 API-TEN-02/03
		authed.GET("/staffs", s.listStaffs)
		authed.POST("/staffs", s.createStaff)
		authed.PUT("/staffs/:id", s.updateStaff)
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
