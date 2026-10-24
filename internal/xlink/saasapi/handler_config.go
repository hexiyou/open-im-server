// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 商户后台：访客接入设置/黑名单/组件代码（API-TEN-07/08/09）+ 日汇总报表

package saasapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson/primitive"
)



/* ---------------- API-TEN-07 访客接入配置 ---------------- */

// configToResp 将 tenant.config JSON 映射为前端友好命名
func configToResp(cfg map[string]any) gin.H {
	get := func(k string) string { v, _ := cfg[k].(string); return v }
	getBool := func(k string) bool { v, _ := cfg[k].(bool); return v }
	widget, _ := cfg["widget"].(map[string]any)
	return gin.H{
		"welcome":        get("welcome"),
		"offlineMsg":     get("offline_msg"),
		"assignStrategy": getOr(cfg, "assign_strategy", "idle_first"),
		"collectMobile":  getBool("collect_mobile"),
		"retentionDays":  getInt(cfg, "guest_retention_days", 30),
		"widget":         widget,
	}
}

func getOr(cfg map[string]any, k, def string) string {
	if v, ok := cfg[k].(string); ok && v != "" {
		return v
	}
	return def
}

func getInt(cfg map[string]any, k string, def int) int {
	if v, ok := cfg[k].(float64); ok {
		return int(v)
	}
	return def
}

func (s *Server) getGuestConfig(c *gin.Context) {
	tenant, err := s.store.GetTenantByUID(c, tenantIDOf(c))
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	apiresp.GinSuccess(c, configToResp(tenant.Config))
}

func (s *Server) putGuestConfig(c *gin.Context) {
	var req struct {
		Welcome        string `json:"welcome"`
		OfflineMsg     string `json:"offlineMsg"`
		AssignStrategy string `json:"assignStrategy"`
		CollectMobile  bool   `json:"collectMobile"`
		RetentionDays  int    `json:"retentionDays"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	if req.AssignStrategy != "idle_first" && req.AssignStrategy != "round_robin" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("assignStrategy must be idle_first/round_robin"))
		return
	}
	if req.RetentionDays <= 0 {
		req.RetentionDays = 30
	}
	merge := map[string]any{
		"welcome":              req.Welcome,
		"offline_msg":          req.OfflineMsg,
		"assign_strategy":      req.AssignStrategy,
		"collect_mobile":       req.CollectMobile,
		"guest_retention_days": req.RetentionDays,
	}
	if err := s.store.UpdateTenantConfig(c, tenantIDOf(c), merge); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "guest-config.update", "", nil)
	apiresp.GinSuccess(c, gin.H{"saved": true})
}

/* ---------------- API-TEN-09 组件嵌入代码 ---------------- */

func (s *Server) getWidgetCode(c *gin.Context) {
	tenant, err := s.store.GetTenantByUID(c, tenantIDOf(c))
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	widget, _ := tenant.Config["widget"].(map[string]any)
	position, _ := widget["position"].(string)
	color, _ := widget["color"].(string)
	bubbleText, _ := widget["bubbleText"].(string)
	if position == "" {
		position = "right"
	}
	if color == "" {
		color = "#2B5BFF"
	}
	if bubbleText == "" {
		bubbleText = "在线咨询"
	}
	code := "<script src=\"https://cdn.xunlink.com/widget.js\" data-tenant=\"" +
		tenant.TenantCode + "\" data-position=\"" + position + "\" data-color=\"" +
		color + "\" data-text=\"" + bubbleText + "\" async></script>"
	apiresp.GinSuccess(c, gin.H{
		"snippet":     code,
		"tenantCode":  tenant.TenantCode,
		"position":    position,
		"color":       color,
		"bubbleText":  bubbleText,
		"guestWidgetOn": tenant.GuestWidgetOn,
	})
}

func (s *Server) putWidgetCode(c *gin.Context) {
	var req struct {
		Position   string `json:"position"`
		Color      string `json:"color"`
		BubbleText string `json:"bubbleText"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	if req.Position != "left" && req.Position != "right" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("position must be left/right"))
		return
	}
	widget := map[string]any{"position": req.Position, "color": req.Color, "bubbleText": req.BubbleText}
	if err := s.store.UpdateTenantConfig(c, tenantIDOf(c), map[string]any{"widget": widget}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "widget.update", "", widget)
	apiresp.GinSuccess(c, gin.H{"saved": true})
}

/* ---------------- API-TEN-08 访客黑名单 ---------------- */

func (s *Server) listBlacklist(c *gin.Context) {
	list, err := s.store.ListGuestBlacklist(c, tenantIDOf(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"list": list, "total": len(list)})
}

func (s *Server) addBlacklist(c *gin.Context) {
	var req struct {
		Fingerprint string `json:"fingerprint"`
		Note        string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Fingerprint) != 64 {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("fingerprint must be 64 hex（从会话详情/访客资料获取）"))
		return
	}
	if err := s.store.AddGuestBlacklist(c, tenantIDOf(c), req.Fingerprint, req.Note); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "blacklist.add", req.Fingerprint[:12]+"…", nil)
	apiresp.GinSuccess(c, gin.H{"added": true})
}

func (s *Server) removeBlacklist(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	if err := s.store.RemoveGuestBlacklist(c, tenantIDOf(c), id); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"removed": true})
}

/* ---------------- 日汇总报表（T-10） ---------------- */

func (s *Server) statsDaily(c *gin.Context) {
	days := 14
	if v := c.Query("days"); v != "" {
		if n, err := time.ParseDuration(v + "h"); err == nil && n > 0 {
			days = int(n.Hours() / 24)
		}
	}
	list, err := s.store.ListQuotaUsage(c, tenantIDOf(c), days)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	// 升序输出便于绘图
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	apiresp.GinSuccess(c, gin.H{"list": list})
}

