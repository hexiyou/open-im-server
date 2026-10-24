// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// admin-api 处理器（API-PLAT-01/03/04/05/06/08/15）

package adminapi

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
)

func timeNowUnixNano() int64 { return time.Now().UnixNano() }

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func pageParams(c *gin.Context) platform.Page {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("size", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	return platform.Page{PageNumber: int32(page), ShowNumber: int32(size)}
}

/* ---------------- API-PLAT-01 登录 ---------------- */

func (s *Server) login(c *gin.Context) {
	// [XunLink] 授权门禁：私有化降级时锁定管理台登录（45101/45102）
	if _, err := s.gate.Check(); err != nil {
		apiresp.GinError(c, err)
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("username/password required"))
		return
	}
	admin, err := s.store.GetPlatformAdminByUsername(c, req.Username)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("account not found"))
		return
	}
	if admin.Status != 1 {
		apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("account disabled"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)) != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("wrong password"))
		return
	}
	token, err := platform.IssueToken(s.cfg.JWT.PlatformSecret, platform.IssuerPlatform, 0, admin.Username, admin.Role, 0, s.cfg.JWT.ExpireDays)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	_ = s.store.TouchPlatformAdminLogin(c, admin.ID)
	_ = s.store.InsertAudit(c, &platform.AuditLog{
		ActorType: "platform", ActorName: admin.Username, Action: "platform.login", IP: c.ClientIP(),
	})
	apiresp.GinSuccess(c, gin.H{"token": token, "role": admin.Role, "account": admin.Username})
}

/* ---------------- API-PLAT-03/04 租户列表/创建 ---------------- */

func (s *Server) listTenants(c *gin.Context) {
	status := int32(-1)
	if v := c.Query("status"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			status = int32(n)
		}
	}
	total, list, err := s.store.ListTenants(c, status, c.Query("keyword"), pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

func (s *Server) createTenant(c *gin.Context) {
	var req struct {
		TenantCode     string `json:"tenantCode"`
		Name           string `json:"name"`
		Industry       string `json:"industry"`
		PlanID         string `json:"planID"`
		SeatsPurchased int32  `json:"seatsPurchased"`
		ExpireDays     int    `json:"expireDays"`
		AdminUsername  string `json:"adminUsername"`
		AdminPassword  string `json:"adminPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	if req.TenantCode == "" || req.Name == "" || req.AdminUsername == "" || req.AdminPassword == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/name/adminUsername/adminPassword required"))
		return
	}
	if _, err := s.store.GetTenantByCode(c, req.TenantCode); err == nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode already exists"))
		return
	}
	tenant := &platform.Tenant{
		TenantID:       0,
		TenantCode:     req.TenantCode,
		Name:           req.Name,
		Industry:       req.Industry,
		SeatsPurchased: req.SeatsPurchased,
		Status:         1,
		GuestWidgetOn:  true,
		Config: map[string]any{
			"welcome":         "您好，欢迎光临～",
			"offline_msg":     "客服不在线，请留言",
			"assign_strategy": "idle_first",
		},
	}
	if req.PlanID != "" {
		if pid, err := primitive.ObjectIDFromHex(req.PlanID); err == nil {
			tenant.PlanID = pid
		}
	}
	if req.ExpireDays > 0 {
		tenant.ExpireAt = time.Now().AddDate(0, 0, req.ExpireDays)
	}
	if err := s.store.CreateTenant(c, tenant); err != nil {
		apiresp.GinError(c, err)
		return
	}
	// 初始化租户管理员账号
	hash, err := hashPassword(req.AdminPassword)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	tenantAdmin := &platform.TenantAdmin{
		TenantID:     tenant.TenantUID,
		Username:     req.AdminUsername,
		PasswordHash: hash,
		Role:         "tenant_admin",
		Status:       1,
	}
	if err := s.store.CreateTenantAdmin(c, tenantAdmin); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "tenant.create", req.TenantCode, gin.H{"name": req.Name, "seats": req.SeatsPurchased})
	apiresp.GinSuccess(c, gin.H{
		"tenantID":  tenant.TenantUID,
		"tenantCode": tenant.TenantCode,
		"_id":       tenant.ID.Hex(),
	})
}

/* ---------------- API-PLAT-05/06 租户更新/详情 ---------------- */

func (s *Server) updateTenant(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		Name           *string `json:"name"`
		Status         *int32  `json:"status"`
		PlanID         *string `json:"planID"`
		SeatsPurchased *int32  `json:"seatsPurchased"`
		ExpireDays     *int    `json:"expireDays"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.SeatsPurchased != nil {
		updates["seats_purchased"] = *req.SeatsPurchased
	}
	if req.PlanID != nil && *req.PlanID != "" {
		if pid, err := primitive.ObjectIDFromHex(*req.PlanID); err == nil {
			updates["plan_id"] = pid
		}
	}
	if req.ExpireDays != nil && *req.ExpireDays > 0 {
		updates["expire_at"] = time.Now().AddDate(0, 0, *req.ExpireDays)
	}
	if err := s.store.UpdateTenant(c, id, updates); err != nil {
		apiresp.GinError(c, err)
		return
	}
	// [XunLink] 停用联动：租户置为停用（status=2）时强制下线其全部成员的 IM 会话
	if req.Status != nil && *req.Status == 2 {
		if tenant, err := s.store.GetTenantByID(c, id); err == nil {
			s.forceLogoutTenantStaffs(tenant.TenantUID)
		}
	}
	s.audit(c, "tenant.update", c.Param("id"), updates)
	apiresp.GinSuccess(c, gin.H{"updated": len(updates)})
}

func (s *Server) tenantDetail(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	tenant, err := s.store.GetTenantByID(c, id)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	var plan *platform.Plan
	if !tenant.PlanID.IsZero() {
		plan, _ = s.store.GetPlan(c, tenant.PlanID)
	}
	staffCount, _ := s.store.CountStaffs(c, tenant.TenantUID)
	apiresp.GinSuccess(c, gin.H{
		"tenant": tenant, "plan": plan,
		"staffCount": staffCount,
	})
}

/* ---------------- API-PLAT-08 套餐 ---------------- */

func (s *Server) listPlans(c *gin.Context) {
	list, err := s.store.ListPlans(c, c.Query("all") != "true")
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"list": list})
}

func (s *Server) createPlan(c *gin.Context) {
	var req struct {
		Name          string         `json:"name"`
		MaxMembers    int32          `json:"maxMembers"`
		MaxSeats      int32          `json:"maxSeats"`
		MaxStorageGB  int32          `json:"maxStorageGB"`
		MaxMonthlyMsg int64          `json:"maxMonthlyMsg"`
		Features      map[string]bool `json:"features"`
		PriceMonth    float64        `json:"priceMonth"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("name required"))
		return
	}
	plan := &platform.Plan{
		Name: req.Name, MaxMembers: req.MaxMembers, MaxSeats: req.MaxSeats,
		MaxStorageGB: req.MaxStorageGB, MaxMonthlyMsg: req.MaxMonthlyMsg,
		Features: req.Features, PriceMonth: req.PriceMonth, OnShelf: true,
	}
	if err := s.store.CreatePlan(c, plan); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "plan.create", req.Name, nil)
	apiresp.GinSuccess(c, plan)
}

/* ---------------- API-PLAT-15 审计 ---------------- */

func (s *Server) listAuditLogs(c *gin.Context) {
	tenantID := int64(-1)
	if v := c.Query("tenantID"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			tenantID = n
		}
	}
	total, list, err := s.store.ListAudits(c, tenantID, c.Query("actorType"), c.Query("action"), pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

/* ---------------- 审计辅助 ---------------- */

func (s *Server) audit(c *gin.Context, action, target string, detail any) {
	d, _ := detail.(map[string]any)
	entry := &platform.AuditLog{
		TenantID:  0,
		ActorType: "platform",
		ActorName: c.GetString("plat_account"),
		Action:    action,
		Target:    target,
		Detail:    d,
		IP:        c.ClientIP(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.store.InsertAudit(ctx, entry)
}
