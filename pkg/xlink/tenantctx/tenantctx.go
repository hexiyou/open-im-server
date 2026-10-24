// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 租户上下文与租户-ID 编解码（XunLink 多租户核心包）
//
// 设计要点（docs/dev/M0-开发日志.md M1 章节）：
//   - userID 编码约定：租户成员/访客为 `t{tenantID}_u{uid}` / `t{tenantID}_g{rand}`；
//     平台级账号（OpenIM 管理员、通知账号等）不带前缀，视为租户 0。
//   - opUserID 始终来源于签名 token（auth rpc ParseToken），不可被客户端伪造，
//     因此 userID 前缀即可作为租户的权威判定依据，任何持有 ctx 的代码均可推导。
//   - 网关层（api/msggateway）会把租户写入 ctx 与 gRPC metadata（经 RpcCustomHeader
//     官方扩展点透传），未写入时 GetTenantID 自动回退到解析 opUserID 前缀。

package tenantctx

import (
	"context"
	"strconv"
	"strings"

	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mcontext"
)

// TenantIDKey ctx/gRPC metadata 中的租户键（gRPC metadata 键必须小写）
const TenantIDKey = "xl-tenant-id"

// HeaderTenantID HTTP 网关层租户头（平台管理员跨租户代操作时使用）
const HeaderTenantID = "X-Tenant-Id"

// PlatformTenantID 平台租户（OpenIM 管理员/通知账号/上游原生数据）
const PlatformTenantID int64 = 0

// ErrCrossTenant 跨租户访问拒绝（API 错误码 40302）
var ErrCrossTenant = errs.NewCodeError(40302, "cross tenant operation denied")

// WithTenantID 显式写入租户（服务间调用构造 ctx 时使用；值为 []string
// 以满足 tools gRPC 拦截器 getRpcContext 对自定义头的取值约定）
func WithTenantID(ctx context.Context, tenantID int64) context.Context {
	ctx = context.WithValue(ctx, TenantIDKey, []string{strconv.FormatInt(tenantID, 10)})
	// 借用 tools 官方扩展点把键透传到下游 RPC（服务端拦截器会原样恢复为 []string）
	keys, _ := ctx.Value(constant.RpcCustomHeader).([]string)
	for _, k := range keys {
		if k == TenantIDKey {
			return ctx
		}
	}
	return context.WithValue(ctx, constant.RpcCustomHeader, append(keys, TenantIDKey))
}

// GetTenantID 解析租户ID（解析顺序：显式值 → gRPC metadata 恢复值 → opUserID 前缀 → 平台0）
func GetTenantID(ctx context.Context) int64 {
	switch v := ctx.Value(TenantIDKey).(type) {
	case string:
		if tid, err := strconv.ParseInt(v, 10, 64); err == nil {
			return tid
		}
	case []string:
		if len(v) == 1 {
			if tid, err := strconv.ParseInt(v[0], 10, 64); err == nil {
				return tid
			}
		}
	case int64:
		return v
	}
	if opUserID := mcontext.GetOpUserID(ctx); opUserID != "" {
		if tid, ok := ParseTenantFromUserID(opUserID); ok {
			return tid
		}
	}
	return PlatformTenantID
}

// EncodeUserID 生成带租户前缀的 OpenIM userID
func EncodeUserID(tenantID int64, uid string) string {
	return "t" + strconv.FormatInt(tenantID, 10) + "_" + uid
}

// ParseTenantFromUserID 从 userID 解析租户ID；非 `t{tid}_` 前缀（平台账号）返回 (0, false)
func ParseTenantFromUserID(userID string) (int64, bool) {
	if !strings.HasPrefix(userID, "t") {
		return PlatformTenantID, false
	}
	rest := userID[1:]
	idx := strings.IndexByte(rest, '_')
	if idx <= 0 {
		return PlatformTenantID, false
	}
	tid, err := strconv.ParseInt(rest[:idx], 10, 64)
	if err != nil || tid <= 0 {
		return PlatformTenantID, false
	}
	return tid, true
}

// CheckSameTenant 校验两个 userID 属于同一租户（平台账号可与任意租户互通），
// 不满足时返回 ErrCrossTenant（错误码 40302）。
func CheckSameTenant(userIDA, userIDB string) error {
	ta, _ := ParseTenantFromUserID(userIDA)
	tb, _ := ParseTenantFromUserID(userIDB)
	if ta == PlatformTenantID || tb == PlatformTenantID || ta == tb {
		return nil
	}
	return ErrCrossTenant.WrapMsg("userIDA", userIDA, "userIDB", userIDB)
}

// ScopeKey 租户作用域缓存键（T-93 约定的唯一入口）：
// 租户上下文 >0 → `t{tid}:{key}`；平台租户 → `global:{key}`。
// 新增 Redis/缓存键必须经过本函数，禁止直接拼全局裸键。
func ScopeKey(ctx context.Context, key string) string {
	if tid := GetTenantID(ctx); tid > PlatformTenantID {
		return "t" + strconv.FormatInt(tid, 10) + ":" + key
	}
	return "global:" + key
}

// CheckTenantUser 校验 userID 属于 ctx 的租户（平台租户放行所有）
func CheckTenantUser(ctx context.Context, userID string) error {
	tenantID := GetTenantID(ctx)
	if tenantID == PlatformTenantID {
		return nil
	}
	if uidTenant, _ := ParseTenantFromUserID(userID); uidTenant != tenantID {
		return ErrCrossTenant.WrapMsg("userID", userID, "ctxTenant", tenantID)
	}
	return nil
}
