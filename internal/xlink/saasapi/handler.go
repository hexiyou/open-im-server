// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// saas-api 处理器（API-TEN-01/02/03/16）

package saasapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/protocol/sdkws"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
)

func timeNowUnixNano() int64 { return time.Now().UnixNano() }

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

// newUserClient / newUserRegisterReq 访客与成员注册共用的 gRPC 便捷封装
func newUserClient(conn grpc.ClientConnInterface) pbuser.UserClient {
	return pbuser.NewUserClient(conn)
}

func newUserRegisterReq(userID, nickname string) *pbuser.UserRegisterReq {
	return &pbuser.UserRegisterReq{
		Users: []*sdkws.UserInfo{{UserID: userID, Nickname: nickname}},
	}
}

func tenantIDOf(c *gin.Context) int64 {
	v, _ := c.Get("tenant_id")
	tid, _ := v.(int64)
	return tid
}

// audit 商户后台操作审计（统一入口）
func (s *Server) audit(c *gin.Context, action, target string, detail map[string]any) {
	entry := &platform.AuditLog{
		TenantID:  tenantIDOf(c),
		ActorType: "tenant",
		ActorName: c.GetString("tenant_account"),
		Action:    action,
		Target:    target,
		Detail:    detail,
		IP:        c.ClientIP(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.store.InsertAudit(ctx, entry)
}

/* ---------------- API-TEN-01 商户后台登录 ---------------- */

func (s *Server) login(c *gin.Context) {
	var req struct {
		TenantCode string `json:"tenantCode"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.TenantCode == "" || req.Username == "" || req.Password == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("tenantCode/username/password required"))
		return
	}
	tenant, err := s.store.GetTenantByCode(c, req.TenantCode)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
		return
	}
	if tenant.Status != 1 {
		apiresp.GinError(c, errs.ErrTokenInvalid.WrapMsg("tenant disabled"))
		return
	}
	admin, err := s.store.GetTenantAdmin(c, tenant.TenantUID, req.Username)
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
	token, err := platform.IssueToken(s.cfg.JWT.TenantSecret, platform.IssuerTenant, 0, admin.Username, admin.Role, tenant.TenantUID, s.cfg.JWT.ExpireDays)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	_ = s.store.TouchTenantAdminLogin(c, admin.ID)
	_ = s.store.InsertAudit(c, &platform.AuditLog{
		TenantID: tenant.TenantUID, ActorType: "tenant", ActorName: admin.Username,
		Action: "tenant.login", IP: c.ClientIP(),
	})
	apiresp.GinSuccess(c, gin.H{"token": token, "role": admin.Role, "account": admin.Username, "tenantID": tenant.TenantUID})
}

/* ---------------- API-TEN-02/03 成员管理 ---------------- */

func (s *Server) listStaffs(c *gin.Context) {
	total, list, err := s.store.ListStaffs(c, tenantIDOf(c), pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

// createStaff 添加客服成员：生成 t{tid}_u{rand} 的 IM 账号（gRPC 调 OpenIM 注册）+ staff 文档
func (s *Server) createStaff(c *gin.Context) {
	var req struct {
		Name   string `json:"name"`
		Mobile string `json:"mobile"`
		Role   string `json:"role"`
		SeatOn *bool  `json:"seatOn"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("name required"))
		return
	}
	if req.Role == "" {
		req.Role = "agent"
	}
	if req.Role != "admin" && req.Role != "class_leader" && req.Role != "agent" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid role"))
		return
	}
	tid := tenantIDOf(c)
	seatOn := true
	if req.SeatOn != nil {
		seatOn = *req.SeatOn
	}

	// [XunLink] 席位硬限制（API 错误码 40301）：在职占用席位成员数不得超已购席位
	if seatOn {
		tenant, err := s.store.GetTenantByUID(c, tid)
		if err != nil {
			apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
			return
		}
		used, err := s.store.CountSeatOnStaffs(c, tid)
		if err != nil {
			apiresp.GinError(c, err)
			return
		}
		if used >= int64(tenant.SeatsPurchased) {
			apiresp.GinError(c, errs.NewCodeError(40301, "seat limit exceeded"))
			return
		}
	}

	// 生成 IM userID：t{tid}_u{rand8}
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		apiresp.GinError(c, err)
		return
	}
	imUserID := tenantctx.EncodeUserID(tid, "u"+hex.EncodeToString(buf))

	// 初始密码（一次性返回；bcrypt 入库，/c/auth/login 凭据）
	pwdBytes := make([]byte, 4)
	if _, err := rand.Read(pwdBytes); err != nil {
		apiresp.GinError(c, err)
		return
	}
	initialPassword := "Xl" + hex.EncodeToString(pwdBytes)
	pwHash, err := bcrypt.GenerateFromPassword([]byte(initialPassword), bcrypt.DefaultCost)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}

	// 经 gRPC 注册 OpenIM 用户（ctx：opUserID=平台管理员 以通过 CheckAdmin，租户=本租户）
	opCtx := mcontext.WithOpUserIDContext(mcontext.NewCtx(c.GetHeader("operationID")), s.cfg.OpenIM.IMAdminUserID)
	opCtx = tenantctx.WithTenantID(opCtx, tid)
	userClient := pbuser.NewUserClient(s.userConn)
	_, err = userClient.UserRegister(opCtx, &pbuser.UserRegisterReq{
		Users: []*sdkws.UserInfo{{UserID: imUserID, Nickname: req.Name}},
	})
	if err != nil {
		apiresp.GinError(c, errs.WrapMsg(err, "register openim user failed"))
		return
	}

	staff := &platform.Staff{
		TenantID:     tid,
		IMUserID:     imUserID,
		Name:         req.Name,
		Mobile:       req.Mobile,
		Role:         req.Role,
		SeatOn:       seatOn,
		PasswordHash: string(pwHash),
		Status:       1,
	}
	if err := s.store.CreateStaff(c, staff); err != nil {
		apiresp.GinError(c, err)
		return
	}
	auditCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.store.InsertAudit(auditCtx, &platform.AuditLog{
		TenantID: tid, ActorType: "tenant", ActorName: c.GetString("tenant_account"),
		Action: "staff.create", Target: imUserID, IP: c.ClientIP(),
	})
	apiresp.GinSuccess(c, gin.H{
		"_id": staff.ID.Hex(), "imUserID": imUserID,
		"initialPassword": initialPassword, // 仅此一次返回
	})
}

func (s *Server) updateStaff(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		Status *int32 `json:"status"`
		Role   *string `json:"role"`
		SeatOn *bool  `json:"seatOn"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	updates := map[string]any{}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.SeatOn != nil && *req.SeatOn {
		// [XunLink] 席位硬限制：从空位转占用时校验（40301）
		tenant, err := s.store.GetTenantByUID(c, tenantIDOf(c))
		if err != nil {
			apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("tenant not found"))
			return
		}
		used, err := s.store.CountSeatOnStaffs(c, tenantIDOf(c))
		if err != nil {
			apiresp.GinError(c, err)
			return
		}
		if used >= int64(tenant.SeatsPurchased) {
			apiresp.GinError(c, errs.NewCodeError(40301, "seat limit exceeded"))
			return
		}
		updates["seat_on"] = true
	} else if req.SeatOn != nil {
		updates["seat_on"] = false
	}
	if err := s.store.UpdateStaff(c, tenantIDOf(c), id, updates); err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"updated": len(updates)})
}

/* ---------------- API-TEN-16 配额 ---------------- */

func (s *Server) quota(c *gin.Context) {
	tid := tenantIDOf(c)
	staffCount, _ := s.store.CountStaffs(c, tid)
	// 租户信息按 tenantUID 反查（同一 Store 提供）
	apiresp.GinSuccess(c, gin.H{
		"tenantID":    tid,
		"staffActive": staffCount,
	})
}
