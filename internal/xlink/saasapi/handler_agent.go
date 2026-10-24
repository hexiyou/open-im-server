// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 坐席工作台接口（API-C-04~07/10，IM token 鉴权）
// 在线状态：/c/agent/heartbeat 心跳续期 ZSET `t{tid}:online_agents`（score=过期时间戳），
// 坐席调度 PickAgent 只在心跳存活名单内选择。

package saasapi

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

const agentOnlineTTL = 120 // 心跳有效秒数（客户端每 30~60s 心跳一次）

// imAuthMiddleware 坐席/成员端鉴权：解析本项目签发的 IM token（含 tenantID claim），
// 校验 staff 身份与席位，注入 ctx 供 gRPC 透传
func (s *Server) imAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.Request.Header.Get("token")
		if token == "" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("missing token"))
			c.Abort()
			return
		}
		claims, err := authverify.ParseXunLinkToken(token, s.cfg.OpenIM.TokenSecret)
		if err != nil {
			apiresp.GinError(c, err)
			c.Abort()
			return
		}
		tenantID := claims.TenantID
		if tenantID == 0 {
			if tid, ok := tenantctx.ParseTenantFromUserID(claims.UserID); ok {
				tenantID = tid
			}
		}
		staff, err := s.store.GetStaffByIMUserID(c, tenantID, claims.UserID)
		if err != nil {
			apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("staff not found"))
			c.Abort()
			return
		}
		if staff.Status != 1 {
			apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("staff disabled"))
			c.Abort()
			return
		}
		c.Set("agent_tenant_id", tenantID)
		c.Set("agent_im_user_id", staff.IMUserID)
		c.Set("agent_name", staff.Name)
		c.Next()
	}
}

func agentCtxOf(c *gin.Context) (int64, string) {
	tid, _ := c.Get("agent_tenant_id")
	uid, _ := c.Get("agent_im_user_id")
	tidV, _ := tid.(int64)
	uidV, _ := uid.(string)
	return tidV, uidV
}

// onlineAgents 读取心跳存活名单（nil 表示 Redis 不可用，调度退化为全池可选）
func (s *Server) onlineAgents(ctx context.Context, tenantID int64) map[string]bool {
	if s.rdb == nil {
		return nil
	}
	key := tenantctx.ScopeKey(tenantctx.WithTenantID(ctx, tenantID), "online_agents")
	members, err := s.rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatFloat(float64(time.Now().Unix()), 'f', 0, 64), Max: "+inf",
	}).Result()
	if err != nil {
		return nil
	}
	m := make(map[string]bool, len(members))
	for _, v := range members {
		m[v] = true
	}
	return m
}

/* ---------------- API-C-10 心跳（在线状态续期） ---------------- */

func (s *Server) agentHeartbeat(c *gin.Context) {
	tid, uid := agentCtxOf(c)
	if s.rdb != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		key := tenantctx.ScopeKey(tenantctx.WithTenantID(ctx, tid), "online_agents")
		s.rdb.ZAdd(ctx, key, redis.Z{Score: float64(time.Now().Unix() + agentOnlineTTL), Member: uid})
	}
	apiresp.GinSuccess(c, gin.H{"online": true, "ttl": agentOnlineTTL})
}

/* ---------------- API-C-04 接待队列 ---------------- */

func (s *Server) agentQueue(c *gin.Context) {
	tid, _ := agentCtxOf(c)
	list, err := s.store.ListQueueingSessions(c, tid)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"list": list, "total": len(list)})
}

/* ---------------- API-C-05 认领排队会话 ---------------- */

func (s *Server) agentClaim(c *gin.Context) {
	tid, uid := agentCtxOf(c)
	var req struct {
		SessionID string `json:"sessionID"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionID == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("sessionID required"))
		return
	}
	sid, err := primitiveFromHex(req.SessionID)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	session, err := s.store.GetAgentSession(c, tid, sid)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("session not found"))
		return
	}
	if session.Status != "queueing" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("session not in queue"))
		return
	}
	if err := s.store.UpdateAgentSession(c, tid, sid, bson.M{
		"agent_im_user_id": uid, "status": "active", "claimed_at": time.Now(),
	}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"claimed": true, "sessionID": req.SessionID})
}

/* ---------------- API-C-06 会话转接 ---------------- */

func (s *Server) agentTransfer(c *gin.Context) {
	tid, uid := agentCtxOf(c)
	var req struct {
		SessionID string `json:"sessionID"`
		ToAgent   string `json:"toAgentIMUserID"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.SessionID == "" || req.ToAgent == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("sessionID/toAgentIMUserID required"))
		return
	}
	// 目标必须是同租户在职坐席（租户前缀一致性 + 坐席池校验）
	if err := tenantctx.CheckSameTenant(uid, req.ToAgent); err != nil {
		apiresp.GinError(c, err)
		return
	}
	target, err := s.store.GetStaffByIMUserID(c, tid, req.ToAgent)
	if err != nil || !target.SeatOn || target.Status != 1 {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("target agent unavailable"))
		return
	}
	sid, err := primitiveFromHex(req.SessionID)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	session, err := s.store.GetAgentSession(c, tid, sid)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("session not found"))
		return
	}
	if session.Status != "active" && session.Status != "queueing" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("session not transferable"))
		return
	}
	if err := s.store.UpdateAgentSession(c, tid, sid, bson.M{
		"agent_im_user_id": req.ToAgent, "status": "active", "claimed_at": time.Now(),
	}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.store.InsertAudit(auditCtx, &platform.AuditLog{
		TenantID: tid, ActorType: "tenant", ActorName: c.GetString("agent_name"),
		Action: "session.transfer", Target: req.SessionID,
		Detail: map[string]any{"from": uid, "to": req.ToAgent}, IP: c.ClientIP(),
	})
	apiresp.GinSuccess(c, gin.H{"transferred": true})
}

/* ---------------- API-C-07 结束会话 ---------------- */

func (s *Server) agentCloseSession(c *gin.Context) {
	tid, _ := agentCtxOf(c)
	sid, err := primitiveFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if err := s.store.UpdateAgentSession(c, tid, sid, bson.M{
		"status": "closed", "closed_at": time.Now(),
	}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"closed": true})
}

/* ---------------- 工具 ---------------- */

func primitiveFromHex(hex string) (primitive.ObjectID, error) {
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		return id, errs.ErrArgs.WrapMsg("invalid id")
	}
	return id, nil
}
