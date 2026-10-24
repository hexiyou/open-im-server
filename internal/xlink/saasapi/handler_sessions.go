// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 商户后台接待记录（API-TEN-10/11）与访客满意度（API-G-05）

package saasapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
)

/* ---------------- API-TEN-10 接待记录列表 ---------------- */

func (s *Server) listSessions(c *gin.Context) {
	total, list, err := s.store.ListSessions(c, tenantIDOf(c), c.Query("status"), c.Query("agent"), pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

/* ---------------- API-TEN-11 会话聊天记录 ---------------- */

func (s *Server) sessionMessages(c *gin.Context) {
	tid := tenantIDOf(c)
	sid, err := primitiveFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	session, err := s.store.GetAgentSession(c, tid, sid)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("session not found"))
		return
	}
	if session.ConversationID == "" {
		apiresp.GinSuccess(c, gin.H{"list": []any{}, "total": 0, "note": "queueing session, no conversation"})
		return
	}
	msgs, err := s.store.GetMessagesByConversation(c, tid, session.ConversationID, 200)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	list := make([]gin.H, 0, len(msgs))
	for _, m := range msgs {
		if m.Msg == nil {
			continue
		}
		list = append(list, gin.H{
			"seq":         m.Msg.Seq,
			"sendID":      m.Msg.SendID,
			"recvID":      m.Msg.RecvID,
			"contentType": m.Msg.ContentType,
			"content":     m.Msg.Content,
			"sendTime":    m.Msg.SendTime,
			"tenantID":    m.TenantID,
		})
	}
	apiresp.GinSuccess(c, gin.H{"list": list, "total": len(list), "conversationID": session.ConversationID})
}

/* ---------------- API-G-05 访客满意度评价 ---------------- */

func (s *Server) guestRate(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		HistoryKey string `json:"historyKey"`
		Rating     int32  `json:"rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" || req.HistoryKey == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/historyKey required"))
		return
	}
	if req.Rating < 1 || req.Rating > 5 {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("rating must be 1-5"))
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	guest, err := s.store.GetGuestByHistoryKey(c, tenant.TenantUID, req.HistoryKey)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("historyKey invalid"))
		return
	}
	session, err := s.store.GetLatestSessionByGuest(c, tenant.TenantUID, guest.IMUserID)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("session not found"))
		return
	}
	updates := map[string]any{"rating": req.Rating, "status": "closed", "closed_at": time.Now()}
	if err := s.store.UpdateAgentSession(c, tenant.TenantUID, session.ID, updates); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"rated": true, "rating": req.Rating})
}
