// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 一机一码授权核心库（docs/02 §4）：
//   申请码 = base64url(JSON{merchantName,machineFP,ts})           [结构校验，离线传递]
//   激活码 = base64url(LicensePayload JSON) + "." + base64url(RSA-SHA256 签名)
//   状态机：Unactivated → Activated → (Grace) → Degraded；Revoked → Degraded
//
// RSA 私钥仅存平台侧（admin-api 首次启动自动生成并落盘）；公钥随安装包分发用于验签。

package license

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/openimsdk/tools/errs"
)

// 状态常量（与数据库字典 T-04 status 一致）
const (
	StatusUnactivated = "unactivated"
	StatusActivated   = "activated"
	StatusGrace       = "grace"
	StatusDegraded    = "degraded"
	StatusRevoked     = "revoked"
)

const IssuerName = "XunLink Platform"

// LicensePayload 激活码载荷（docs/02 §4.2）
type LicensePayload struct {
	LicenseID   string   `json:"licenseID"`   // 平台授权编号 T-04.license_no
	Merchant    string   `json:"merchant"`    // 商户名
	TenantCode  string   `json:"tenantCode"`  // 可选，绑定租户编码
	MachineFP   string   `json:"machineFP"`   // 机器指纹（一机一码绑定）
	IssuedAt    int64    `json:"issuedAt"`    // 签发时间戳（秒）
	NotBefore   int64    `json:"notBefore"`   // 生效时间
	NotAfter    int64    `json:"notAfter"`    // 到期时间
	GraceDays   int      `json:"graceDays"`   // 离线宽限天数（在线模式为心跳断连宽限）
	Seats       int      `json:"seats"`       // 席位上限
	Features    []string `json:"features"`    // 功能开关
	Mode        string   `json:"mode"`        // online / offline
	Environment string   `json:"environment"` // production / development
}

// ApplyCode 商户申请码载荷（M2 简化：结构校验，传输信任靠线下渠道；签名硬化列 M4）
type ApplyCode struct {
	Merchant    string `json:"merchantName"`
	MachineFP   string `json:"machineFP"`
	RequestedAt int64  `json:"requestedAt"`
}

// ErrLicenseDegraded 授权失效（错误码 45101，API 字典 §6）
var ErrLicenseDegraded = errs.NewCodeError(45101, "license invalid, service degraded")

// ErrLicenseGrace 授权宽限期（错误码 45102，响应携带剩余天数）
var ErrLicenseGrace = errs.NewCodeError(45102, "license in grace period")

/* ---------------- 申请码 ---------------- */

func EncodeApplyCode(a *ApplyCode) string {
	b, _ := json.Marshal(a)
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeApplyCode(code string) (*ApplyCode, error) {
	b, err := base64.RawURLEncoding.DecodeString(code)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("apply code malformed")
	}
	var a ApplyCode
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, errs.ErrArgs.WrapMsg("apply code malformed")
	}
	if a.MachineFP == "" || len(a.MachineFP) != 64 {
		return nil, errs.ErrArgs.WrapMsg("apply code missing valid machineFP")
	}
	return &a, nil
}

/* ---------------- 激活码签发/验签 ---------------- */

// SignLicense 平台侧签发：payload + RSA 私钥签名 → 激活码文本
func SignLicense(p *LicensePayload, privateKey *rsa.PrivateKey) (string, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return "", errs.Wrap(err)
	}
	digest := sha256.Sum256(b)
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", errs.Wrap(err)
	}
	return base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifyLicense 验签并还原 payload（公钥随安装包分发）
func VerifyLicense(code string, publicKey *rsa.PublicKey) (*LicensePayload, error) {
	code = strings.TrimSpace(code)
	dot := strings.IndexByte(code, '.')
	if dot <= 0 || dot == len(code)-1 {
		return nil, errs.ErrArgs.WrapMsg("activation code malformed")
	}
	b64, s64 := code[:dot], code[dot+1:]
	payloadBytes, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("activation code payload malformed")
	}
	sig, err := base64.RawURLEncoding.DecodeString(s64)
	if err != nil {
		return nil, errs.ErrArgs.WrapMsg("activation code signature malformed")
	}
	digest := sha256.Sum256(payloadBytes)
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], sig); err != nil {
		return nil, errs.ErrArgs.WrapMsg("activation code signature verify failed")
	}
	var p LicensePayload
	if err := json.Unmarshal(payloadBytes, &p); err != nil {
		return nil, errs.ErrArgs.WrapMsg("payload malformed")
	}
	return &p, nil
}

/* ---------------- 状态判定 ---------------- */

// LocalState 本地授权运行时状态
type LocalState struct {
	Status       string `json:"status"`                  // activated / grace / degraded / unactivated / revoked
	Reason       string `json:"reason,omitempty"`
	LastOKHB     int64  `json:"lastOKHeartbeat"`         // 最近一次成功心跳（秒）；offline 模式不适用
	GraceUntil   int64  `json:"graceUntil,omitempty"`    // 宽限截止（秒）
	DegradedAt   int64  `json:"degradedAt,omitempty"`
}

// Evaluate 依据激活码与本地心跳记录判定当前状态
// now: 当前时间戳；revokedRemote: 平台侧已吊销（心跳得知）
func Evaluate(p *LicensePayload, st *LocalState, now int64) (string, int, error) {
	if p == nil {
		return StatusUnactivated, 0, ErrLicenseDegraded
	}
	if st != nil && st.Status == StatusRevoked {
		return StatusRevoked, 0, ErrLicenseDegraded.WrapMsg("revoked by platform")
	}
	if now < p.NotBefore {
		return StatusUnactivated, 0, ErrLicenseDegraded.WrapMsg("license not effective yet")
	}
	if now > p.NotAfter {
		return StatusDegraded, 0, ErrLicenseDegraded.WrapMsg("license expired")
	}
	// 离线模式：本地验签即放行
	if p.Mode == "offline" {
		return StatusActivated, 0, nil
	}
	// 在线模式：心跳必须存活（lastOKHB + graceDays 宽限）
	if st == nil || st.LastOKHB == 0 {
		// 从未心跳过：视为刚激活，宽限从激活时刻起算
		if st == nil || st.GraceUntil == 0 {
			return StatusGrace, int(p.GraceDays), ErrLicenseGrace
		}
	}
	deadline := p.NotAfter
	if st != nil && st.LastOKHB > 0 {
		hbDeadline := st.LastOKHB + int64(p.GraceDays)*86400
		if hbDeadline < deadline {
			deadline = hbDeadline
		}
	}
	if now > deadline {
		return StatusDegraded, 0, ErrLicenseDegraded.WrapMsg("heartbeat missed beyond grace period")
	}
	remaining := int((deadline - now) / 86400)
	if st != nil && st.LastOKHB > 0 && st.LastOKHB+int64(p.GraceDays)*86400 < p.NotAfter {
		// 在宽限期内（有历史心跳但当前心跳失败）
		if st.Status == StatusGrace {
			return StatusGrace, remaining, ErrLicenseGrace
		}
	}
	return StatusActivated, remaining, nil
}
