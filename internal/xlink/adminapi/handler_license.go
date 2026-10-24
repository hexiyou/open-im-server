// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 一机一码授权接口（API-PLAT-09/10/11/12/17/18 + API-SYS-01/02，docs/02 §4）
// 签发流程：商户装机 → license-cli apply 生成申请码 → 平台 decode/issue 签发激活码
//          → 商户 license-cli activate 验签激活 → 在线模式心跳（吊销/过期即降级）

package adminapi

import (

	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/license"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

/* ---------------- API-PLAT-18 解析申请码（申请队列预览） ---------------- */

func (s *Server) decodeApply(c *gin.Context) {
	var req struct {
		ApplyCode string `json:"applyCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ApplyCode == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("applyCode required"))
		return
	}
	apply, err := license.DecodeApplyCode(req.ApplyCode)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{
		"merchantName": apply.Merchant,
		"machineFP":    apply.MachineFP,
		"requestedAt":  apply.RequestedAt,
	})
}

/* ---------------- API-PLAT-10 签发激活码 ---------------- */

func (s *Server) issueLicense(c *gin.Context) {
	var req struct {
		ApplyCode    string `json:"applyCode"`
		MerchantName string `json:"merchantName"` // 可省略（默认取申请码内）
		TenantCode   string `json:"tenantCode"`
		ValidityDays int    `json:"validityDays"`
		Seats        int    `json:"seats"`
		GraceDays    int    `json:"graceDays"`
		Mode         string `json:"mode"` // online / offline
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ApplyCode == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("applyCode required"))
		return
	}
	apply, err := license.DecodeApplyCode(req.ApplyCode)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	merchant := req.MerchantName
	if merchant == "" {
		merchant = apply.Merchant
	}
	if merchant == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("merchantName required"))
		return
	}
	if req.ValidityDays <= 0 {
		req.ValidityDays = 365
	}
	if req.Seats <= 0 {
		req.Seats = 5
	}
	if req.GraceDays < 0 {
		req.GraceDays = 7
	}
	if req.Mode == "" {
		req.Mode = "online"
	}
	if req.Mode != "online" && req.Mode != "offline" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("mode must be online/offline"))
		return
	}

	now := time.Now().Unix()
	licenseNo := fmt.Sprintf("L%d%s", now, randHex(2))
	payload := &license.LicensePayload{
		LicenseID:   licenseNo,
		Merchant:    merchant,
		TenantCode:  req.TenantCode,
		MachineFP:   apply.MachineFP,
		IssuedAt:    now,
		NotBefore:   now,
		NotAfter:    time.Now().AddDate(0, 0, req.ValidityDays).Unix(),
		GraceDays:   req.GraceDays,
		Seats:       req.Seats,
		Features:    []string{"single_chat", "guest_chat", "voice"},
		Mode:        req.Mode,
		Environment: "production",
	}
	code, err := license.SignLicense(payload, s.privateKey)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	rec := &platform.License{
		LicenseNo:      licenseNo,
		MerchantName:   merchant,
		TenantCode:     req.TenantCode,
		MachineFP:      apply.MachineFP,
		ApplyCode:      req.ApplyCode,
		ActivationCode: code,
		NotBefore:      payload.NotBefore,
		NotAfter:       payload.NotAfter,
		GraceDays:      int32(req.GraceDays),
		Seats:          int32(req.Seats),
		Features:       payload.Features,
		Mode:           req.Mode,
		Status:         license.StatusUnactivated,
	}
	if err := s.store.CreateLicense(c, rec); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "license.issue", licenseNo, gin.H{"merchant": merchant, "fp": apply.MachineFP, "mode": req.Mode})
	apiresp.GinSuccess(c, gin.H{
		"licenseNo":      licenseNo,
		"activationCode": code,
		"payload":        payload,
	})
}

/* ---------------- API-PLAT-09 授权列表 ---------------- */

func (s *Server) listLicenses(c *gin.Context) {
	total, list, err := s.store.ListLicenses(c, c.Query("status"), pageParams(c))
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, gin.H{"total": total, "list": list})
}

/* ---------------- API-PLAT-11 吊销 ---------------- */

func (s *Server) revokeLicense(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		ConfirmMerchantName string `json:"confirmMerchantName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ConfirmMerchantName == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("confirmMerchantName required"))
		return
	}
	rec, err := s.store.GetLicenseByID(c, id)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("license not found"))
		return
	}
	if rec.MerchantName != req.ConfirmMerchantName {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("confirm name mismatch"))
		return
	}
	if err := s.store.UpdateLicense(c, id, bson.M{"status": license.StatusRevoked}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "license.revoke", rec.LicenseNo, nil)
	apiresp.GinSuccess(c, gin.H{"revoked": true})
}

/* ---------------- API-PLAT-12 换机重绑 ---------------- */

func (s *Server) rebindLicense(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		NewApplyCode string `json:"newApplyCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.NewApplyCode == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("newApplyCode required"))
		return
	}
	rec, err := s.store.GetLicenseByID(c, id)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("license not found"))
		return
	}
	apply, err := license.DecodeApplyCode(req.NewApplyCode)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	now := time.Now().Unix()
	payload := &license.LicensePayload{
		LicenseID:   rec.LicenseNo,
		Merchant:    rec.MerchantName,
		TenantCode:  rec.TenantCode,
		MachineFP:   apply.MachineFP,
		IssuedAt:    now,
		NotBefore:   rec.NotBefore,
		NotAfter:    rec.NotAfter,
		GraceDays:   int(rec.GraceDays),
		Seats:       int(rec.Seats),
		Features:    rec.Features,
		Mode:        rec.Mode,
		Environment: "production",
	}
	code, err := license.SignLicense(payload, s.privateKey)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	if err := s.store.UpdateLicense(c, id, bson.M{
		"machine_fp": apply.MachineFP, "apply_code": req.NewApplyCode,
		"activation_code": code, "status": license.StatusUnactivated,
	}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "license.rebind", rec.LicenseNo, gin.H{"newFP": apply.MachineFP})
	apiresp.GinSuccess(c, gin.H{"activationCode": code})
}

/* ---------------- API-PLAT-17 续期 ---------------- */

func (s *Server) renewLicense(c *gin.Context) {
	id, err := primitive.ObjectIDFromHex(c.Param("id"))
	if err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("invalid id"))
		return
	}
	var req struct {
		ExtendDays int `json:"extendDays"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.ExtendDays <= 0 {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("extendDays required"))
		return
	}
	rec, err := s.store.GetLicenseByID(c, id)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("license not found"))
		return
	}
	notAfter := rec.NotAfter
	now := time.Now().Unix()
	if notAfter < now {
		notAfter = now // 已过期则从当前时间起算
	}
	notAfter += int64(req.ExtendDays) * 86400
	payload := &license.LicensePayload{
		LicenseID: rec.LicenseNo, Merchant: rec.MerchantName, TenantCode: rec.TenantCode,
		MachineFP: rec.MachineFP, IssuedAt: now,
		NotBefore: rec.NotBefore, NotAfter: notAfter,
		GraceDays: int(rec.GraceDays), Seats: int(rec.Seats), Features: rec.Features,
		Mode: rec.Mode, Environment: "production",
	}
	code, err := license.SignLicense(payload, s.privateKey)
	if err != nil {
		apiresp.GinError(c, err)
		return
	}
	status := rec.Status
	if status == license.StatusDegraded || status == license.StatusRevoked {
		status = license.StatusActivated // 续期即解除降级
	}
	if err := s.store.UpdateLicense(c, id, bson.M{
		"not_after": notAfter, "activation_code": code, "status": status,
	}); err != nil {
		apiresp.GinError(c, err)
		return
	}
	s.audit(c, "license.renew", rec.LicenseNo, gin.H{"extendDays": req.ExtendDays})
	apiresp.GinSuccess(c, gin.H{"activationCode": code, "notAfter": notAfter})
}

/* ---------------- API-SYS-01 心跳（免登录，指纹+授权号定位） ---------------- */

func (s *Server) licenseHeartbeat(c *gin.Context) {
	var req struct {
		MachineFP string `json:"machineFP"`
		LicenseNo string `json:"licenseNo"`
		Version   string `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MachineFP == "" || req.LicenseNo == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("machineFP/licenseNo required"))
		return
	}
	rec, err := s.store.GetLicenseByNoAndFP(c, req.LicenseNo, req.MachineFP)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("license not found"))
		return
	}
	now := time.Now().Unix()
	if rec.Status == license.StatusRevoked {
		apiresp.GinSuccess(c, license.HeartbeatResponse{OK: false, Status: license.StatusRevoked, Reason: "revoked by platform"})
		return
	}
	if now > rec.NotAfter {
		_ = s.store.UpdateLicense(c, rec.ID, bson.M{"status": license.StatusDegraded})
		apiresp.GinSuccess(c, license.HeartbeatResponse{OK: false, Status: license.StatusDegraded, Reason: "expired"})
		return
	}
	updates := bson.M{"last_heartbeat": now, "status": license.StatusActivated}
	if rec.Status == license.StatusUnactivated {
		updates["activated_at"] = now
	}
	_ = s.store.UpdateLicense(c, rec.ID, updates)
	apiresp.GinSuccess(c, license.HeartbeatResponse{OK: true, Status: license.StatusActivated})
}

/* ---------------- API-SYS-02 激活上报 ---------------- */

func (s *Server) licenseActivateReport(c *gin.Context) {
	var req struct {
		MachineFP string `json:"machineFP"`
		LicenseNo string `json:"licenseNo"`
		Version   string `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MachineFP == "" || req.LicenseNo == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("machineFP/licenseNo required"))
		return
	}
	rec, err := s.store.GetLicenseByNoAndFP(c, req.LicenseNo, req.MachineFP)
	if err != nil {
		apiresp.GinError(c, errs.ErrRecordNotFound.WrapMsg("license not found"))
		return
	}
	now := time.Now().Unix()
	_ = s.store.UpdateLicense(c, rec.ID, bson.M{
		"status": license.StatusActivated, "activated_at": now, "last_heartbeat": now,
	})
	apiresp.GinSuccess(c, gin.H{"activated": true})
}

/* ---------------- 授权门禁与状态（/license/status 任何状态可用） ---------------- */

func (s *Server) licenseGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, err := s.gate.Check(); err != nil {
			apiresp.GinError(c, err)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *Server) licenseStatus(c *gin.Context) {
	resp := gin.H{"enabled": s.gate.Enabled()}
	ll, status, err := license.CurrentState(s.cfg.License.LocalDir)
	resp["status"] = status
	if err != nil {
		if cd, ok := errs.Unwrap(err).(errs.CodeError); ok {
			resp["code"] = cd.Code()
		}
		resp["reason"] = err.Error()
	}
	if ll != nil && ll.Payload != nil {
		resp["licenseNo"] = ll.Payload.LicenseID
		resp["merchant"] = ll.Payload.Merchant
		resp["mode"] = ll.Payload.Mode
		resp["notAfter"] = ll.Payload.NotAfter
		resp["seats"] = ll.Payload.Seats
		resp["lastOKHeartbeat"] = ll.State.LastOKHB
	}
	apiresp.GinSuccess(c, resp)
}
