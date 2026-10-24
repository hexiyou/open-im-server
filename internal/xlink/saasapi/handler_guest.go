// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 访客链路处理器（免鉴权区，API-G-01/02/03/04）
// 流程：init 限流/黑名单校验 → 坐席调度(idle_first) → 注册访客 OpenIM 用户 →
//      创建接待记录（会话ID 预计算 si_ 前缀）→ 签发访客 IM token（经 auth rpc）

package saasapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/gin-gonic/gin"
	pbauth "github.com/openimsdk/protocol/auth"
	"github.com/openimsdk/open-im-server/v3/pkg/util/conversationutil"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

const guestPlatformID = int32(3) // 访客走 Web 平台端

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// guestCtx 构造带租户与 operationID 的上下文（gRPC 透传用）
func (s *Server) guestCtx(c *gin.Context, tenantID int64) context.Context {
	ctx := mcontext.NewCtx(c.GetHeader("operationID"))
	ctx = tenantctx.WithTenantID(ctx, tenantID)
	return ctx
}

// rateLimit 访客 init 限流（Redis INCR + 1h TTL，超限 42901）
func (s *Server) rateLimit(c *gin.Context, tenantID int64) error {
	if s.rdb == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := tenantctx.ScopeKey(tenantctx.WithTenantID(ctx, tenantID), "guest:rl:"+c.ClientIP())
	n, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return nil // Redis 异常不阻断业务（降级放行）
	}
	if n == 1 {
		s.rdb.Expire(ctx, key, time.Hour)
	}
	limit := s.cfg.Guest.RateLimitPerHour
	if limit <= 0 {
		limit = 50
	}
	if n > limit {
		return errs.NewCodeError(42901, "too many requests, captcha required")
	}
	return nil
}

// issueIMToken 经 auth rpc 签发 OpenIM token（ctx opUserID=平台管理员 以通过 CheckAdmin）
func (s *Server) issueIMToken(ctx context.Context, userID string) (string, int64, error) {
	opCtx := mcontext.WithOpUserIDContext(ctx, s.cfg.OpenIM.IMAdminUserID)
	authClient := pbauth.NewAuthClient(s.authConn)
	resp, err := authClient.GetUserToken(opCtx, &pbauth.GetUserTokenReq{
		UserID:     userID,
		PlatformID: guestPlatformID,
	})
	if err != nil {
		return "", 0, errs.WrapMsg(err, "issue im token failed")
	}
	return resp.Token, resp.ExpireTimeSeconds, nil
}

/* ---------------- API-G-01 访客初始化 ---------------- */

func (s *Server) guestInit(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		SourceURL  string `json:"sourceURL"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode required"))
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil || tenant.Status != 1 || !tenant.GuestWidgetOn {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant unavailable"))
		return
	}
	// 限流（42901）与黑名单
	if err := s.rateLimit(c, tenant.TenantUID); err != nil {
		apiresp.GinError(c, err)
		return
	}
	fpHash := sha256.Sum256([]byte(c.ClientIP() + "|" + c.Request.Header.Get("User-Agent")))
	fingerprint := hex.EncodeToString(fpHash[:])
	if blocked, _ := s.store.IsGuestBlacklisted(c, tenant.TenantUID, fingerprint); blocked {
		apiresp.GinError(c, errs.NewCodeError(40303, "blocked").WrapMsg("fingerprint blocked"))
		return
	}

	ctx := s.guestCtx(c, tenant.TenantUID)
	// 坐席调度：仅心跳存活（在线）的坐席参与；Redis 不可用时降级为全池
	agent, err := s.store.PickAgent(ctx, tenant.TenantUID, s.onlineAgents(ctx, tenant.TenantUID))
	if err != nil && errs.Unwrap(err) != platform.ErrNoAgentAvailable {
		apiresp.GinError(c, err)
		return
	}
	online := agent != nil
	if !online {
		// 无可用坐席：返回离线形态（前端展示留言表单，走 /g/offline-message）
		apiresp.GinSuccess(c, gin.H{
			"online": false,
			"config": s.guestConfigData(tenant),
		})
		return
	}

	// 注册访客 OpenIM 用户 + 建 guest 文档
	guestIMUserID := tenantctx.EncodeUserID(tenant.TenantUID, "g"+randHex(5))
	opCtx := mcontext.WithOpUserIDContext(ctx, s.cfg.OpenIM.IMAdminUserID)
	userClient := newUserClient(s.userConn)
	if _, err := userClient.UserRegister(opCtx, newUserRegisterReq(guestIMUserID, "访客")); err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "register guest failed"))
		return
	}
	historyKey := randHex(16)
	guest := &platform.Guest{
		TenantID:    tenant.TenantUID,
		IMUserID:    guestIMUserID,
		HistoryKey:  historyKey,
		Fingerprint: fingerprint,
		SourceURL:   req.SourceURL,
		Status:      1,
	}
	if err := s.store.CreateGuest(c, guest); err != nil {
		apiresp.GinError(c, err)
		return
	}
	// 接待记录（会话ID 预计算：与 OpenIM 单聊会话ID 同规则）
	session := &platform.AgentSession{
		TenantID:       tenant.TenantUID,
		GuestIMUserID:  guestIMUserID,
		AgentIMUserID:  agent.IMUserID,
		ConversationID: conversationutil.GenConversationIDForSingle(guestIMUserID, agent.IMUserID),
		SourceURL:      req.SourceURL,
		Status:         "active",
		ClaimedAt:      time.Now(),
	}
	if err := s.store.CreateAgentSession(c, session); err != nil {
		apiresp.GinError(c, err)
		return
	}
	imToken, expire, err := s.issueIMToken(ctx, guestIMUserID)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{
		"online":       true,
		"guestUserID":  guestIMUserID,
		"imToken":      imToken,
		"tokenExpire":  expire,
		"historyKey":   historyKey,
		"agent":        gin.H{"imUserID": agent.IMUserID, "name": agent.Name},
		"config":       s.guestConfigData(tenant),
	})
}

/* ---------------- API-G-02 访客续聊 ---------------- */

func (s *Server) guestRenew(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		HistoryKey string `json:"historyKey"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" || req.HistoryKey == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/historyKey required"))
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil || tenant.Status != 1 {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant unavailable"))
		return
	}
	guest, err := s.store.GetGuestByHistoryKey(c, tenant.TenantUID, req.HistoryKey)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("historyKey invalid"))
		return
	}
	_ = s.store.TouchGuestActive(c, guest.ID)
	ctx := s.guestCtx(c, tenant.TenantUID)
	imToken, expire, err := s.issueIMToken(ctx, guest.IMUserID)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{
		"guestUserID": guest.IMUserID,
		"imToken":     imToken,
		"tokenExpire": expire,
	})
}

/* ---------------- API-G-03 商户公开配置 ---------------- */

func (s *Server) guestConfig(c *gin.Context) {
	tenant, err := s.store.GetTenantByCode(c, c.Query("tenant"))
	if err != nil || tenant.Status != 1 {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant unavailable"))
		return
	}
	apiresp.GinSuccess(c, s.guestConfigData(tenant))
}

func (s *Server) guestConfigData(t *platform.Tenant) gin.H {
	welcome, _ := t.Config["welcome"].(string)
	offline, _ := t.Config["offline_msg"].(string)
	strategy, _ := t.Config["assign_strategy"].(string)
	if strategy == "" {
		strategy = "idle_first"
	}
	return gin.H{
		"tenantCode": t.TenantCode, "name": t.Name,
		"welcome": welcome, "offlineMsg": offline,
		"assignStrategy": strategy, "widgetOn": t.GuestWidgetOn,
	}
}

/* ---------------- API-G-04 离线留言 ---------------- */

func (s *Server) guestOfflineMessage(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		Name       string `json:"name"`
		Mobile     string `json:"mobile"`
		Content    string `json:"content"`
		SourceURL  string `json:"sourceURL"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" || req.Content == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/content required"))
		return
	}
	if err := s.rateLimit(c, tenantUIDOfCode(s, req.TenantCode)); err != nil {
		apiresp.GinError(c, err)
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil || tenant.Status != 1 {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant unavailable"))
		return
	}
	ctx := s.guestCtx(c, tenant.TenantUID)
	guestIMUserID := tenantctx.EncodeUserID(tenant.TenantUID, "g"+randHex(5))
	opCtx := mcontext.WithOpUserIDContext(ctx, s.cfg.OpenIM.IMAdminUserID)
	if _, err := newUserClient(s.userConn).UserRegister(opCtx, newUserRegisterReq(guestIMUserID, "访客留言:"+req.Name)); err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "register guest failed"))
		return
	}
	guest := &platform.Guest{
		TenantID: tenant.TenantUID, IMUserID: guestIMUserID,
		HistoryKey: randHex(16), SourceURL: req.SourceURL, Status: 1,
	}
	if err := s.store.CreateGuest(c, guest); err != nil {
		apiresp.GinError(c, err)
		return
	}
	session := &platform.AgentSession{
		TenantID:       tenant.TenantUID,
		GuestIMUserID:  guestIMUserID,
		ConversationID: "",
		Status:         "queueing",
	}
	if err := s.store.CreateAgentSession(c, session); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"queued": true})
}

// tenantUIDOfCode 便捷函数（离线留言限流用；tenant 不存在返回 -1 使限流键落到 global）
func tenantUIDOfCode(s *Server, code string) int64 {
	t, err := s.store.GetTenantByCode(context.Background(), code)
	if err != nil {
		return -1
	}
	return t.TenantUID
}
