// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层 JWT：总后台（issuer=xlink:platform）与商户后台（issuer=xlink:tenant）
// 两套独立签发/校验，绝不互认（docs/02 §6 安全设计）。

package platform

import (
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/openimsdk/tools/errs"
)

const (
	IssuerPlatform = "xlink:platform"
	IssuerTenant   = "xlink:tenant"
)

type Claims struct {
	UID      int64  `json:"uid"`                // 账号ID（platform_admin / tenant_admin 的 _id 时间戳部分不可用，此处用自增含义的内部ID）
	Account  string `json:"account"`            // 登录名
	Role     string `json:"role"`
	TenantID int64  `json:"tenantID,omitempty"` // 仅商户后台 JWT 携带
	jwt.RegisteredClaims
}

func IssueToken(secret, issuer string, uid int64, account, role string, tenantID int64, expireDays int) (string, error) {
	now := time.Now()
	claims := Claims{
		UID:     uid,
		Account: account,
		Role:    role,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireDays) * 24 * time.Hour)),
			ID:        issuer + ":" + account,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// ParseToken 校验并解析；issuer 不匹配直接拒绝（两类令牌不互认）
func ParseToken(tokenString, secret, expectIssuer string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errs.ErrTokenMalformed.WrapMsg("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, errs.WrapMsg(err, "jwt parse")
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errs.ErrTokenUnknown.WrapMsg("claims invalid")
	}
	if claims.Issuer != expectIssuer {
		return nil, errs.ErrTokenInvalid.WrapMsg("issuer mismatch", "got", claims.Issuer, "want", expectIssuer)
	}
	return claims, nil
}
