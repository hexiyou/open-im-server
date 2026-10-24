// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// admin-api 扩展处理器：平台管理员管理（API-PLAT-16）、租户管理员密码重置（API-PLAT-07）、
// 租户停用联动（auth rpc ForceLogout 强制下线全部成员）

package adminapi

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	pbauth "github.com/openimsdk/protocol/auth"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

/* ---------------- API-PLAT-16 平台管理员管理 ---------------- */

func (s *Server) listAdmins(c *gin.Context) {
	total, list, err := s.store.ListPlatformAdmins(c, pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

func (s *Server) createAdmin(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Username == "" || req.Password == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("username/password required"))
		return
	}
	if req.Role != "super" && req.Role != "ops" && req.Role != "readonly" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("role must be super/ops/readonly"))
		return
	}
	if _, err := s.store.GetPlatformAdminByUsername(c, req.Username); err == nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("username already exists"))
		return
	}
	hash, err := hashPassword(req.Password)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	admin := &platform.PlatformAdmin{Username: req.Username, PasswordHash: hash, Role: req.Role, Status: 1}
	if err := s.store.CreatePlatformAdmin(c, admin); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "platform.admin.create", req.Username, gin.H{"role": req.Role})
	apiresp.GinSuccess(c, gin.H{"_id": admin.ID.Hex(), "username": admin.Username, "role": admin.Role})
}

func (s *Server) updateAdmin(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		Status   *int32  `json:"status"`
		Role     *string `json:"role"`
		Password *string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	updates := bson.M{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Role != nil {
		if *req.Role != "super" && *req.Role != "ops" && *req.Role != "readonly" {
			apiresp.GinError(c, errs.ErrArgs.WrapMsg("role must be super/ops/readonly"))
			return
		}
		updates["role"] = *req.Role
	}
	if req.Password != nil && *req.Password != "" {
		hash, err := hashPassword(*req.Password)
		if err != nil {
			apiresp.GinError(c, err)
			return
		}
		updates["password_hash"] = hash
	}
	if err := s.store.UpdatePlatformAdmin(c, id, updates); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "platform.admin.update", c.Param("id"), nil)
	apiresp.GinSuccess(c, gin.H{"updated": len(updates)})
}

/* ---------------- API-PLAT-07 租户管理员密码重置 ---------------- */

func (s *Server) resetTenantAdminPassword(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		Username   string `json:"username"`   // 省略则重置该租户全部管理员
		NewPassword string `json:"newPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.NewPassword == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("newPassword required"))
		return
	}
	tenant, err := s.store.GetTenantByID(c, id)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	hash, err := hashPassword(req.NewPassword)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	usernames := []string{}
	if req.Username != "" {
		usernames = []string{req.Username}
	}
	if err := s.store.UpdateTenantAdminByUsernames(c, tenant.TenantUID, usernames,
		bson.M{"password_hash": string(hash)}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "tenant.reset-admin-password", tenant.TenantCode, gin.H{"usernames": usernames})
	apiresp.GinSuccess(c, gin.H{"reset": true})
}

/* ---------------- 租户停用联动：强制下线全部成员 ---------------- */

// forceLogoutTenantStaffs 停用租户后调用 auth rpc ForceLogout 踢掉全部成员的 IM 会话
func (s *Server) forceLogoutTenantStaffs(tenantID int64) {
	staffs, err := s.store.ListSeatOnStaffs(context.Background(), tenantID)
	if err != nil {
		return
	}
	authClient := pbauth.NewAuthClient(s.authConn)
	opCtx := mcontext.WithOpUserIDContext(context.Background(), "imAdmin")
	for _, st := range staffs {
		ctx, cancel := context.WithTimeout(opCtx, 3*time.Second)
		_, _ = authClient.ForceLogout(ctx, &pbauth.ForceLogoutReq{UserID: st.IMUserID, PlatformID: 3})
		cancel()
	}
}
