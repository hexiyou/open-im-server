// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 客户端登录（API-C-01）：四端客服/成员凭据登录 → 签发 IM token（经 auth rpc）
// 席位校验：seat_on=false 或席位超购的成员不允许登录客服端（40301）

package saasapi

import (
	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) clientLogin(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		UserID     string `json:"userID"` // imUserID（t{tid}_u{rand}，登录页展示为"讯联号"）
		Password   string `json:"password"`
		PlatformID int32  `json:"platformID"` // 1iOS/2Android/3Windows/4OSX/5Web/6小程序；缺省 5=Web
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" || req.UserID == "" || req.Password == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/userID/password required"))
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil || tenant.Status != 1 {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant unavailable"))
		return
	}
	// [XunLink] userID 必须归属该租户（防跨租户撞库）
	if tid, _ := tenantctx.ParseTenantFromUserID(req.UserID); tid != tenant.TenantUID {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("account not found"))
		return
	}
	staff, err := s.store.GetStaffByIMUserID(c, tenant.TenantUID, req.UserID)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("account not found"))
		return
	}
	if staff.Status != 1 {
		apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("account disabled"))
		return
	}
	// 席位校验：不占席位的成员不可登录聊天客户端（仅后台账号）
	if !staff.SeatOn {
		apiresp.GinError(c, errs.NewCodeError(40301, "seat limit exceeded").WrapMsg("staff not on seat"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(staff.PasswordHash), []byte(req.Password)) != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("wrong password"))
		return
	}
	ctx := s.guestCtx(c, tenant.TenantUID)
	imPlatform := req.PlatformID
	if imPlatform == 0 {
		imPlatform = 5
	}
	imToken, expire, err := s.issueIMToken(ctx, staff.IMUserID, imPlatform)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{
		"token":    imToken,
		"expire":   expire,
		"imUserID": staff.IMUserID,
		"nickname": staff.Name,
		"role":     staff.Role,
	})
}
