// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package authverify

import (
	"context"
	"fmt"

	"github.com/golang-jwt/jwt/v4"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/mcontext"
	"github.com/openimsdk/tools/tokenverify"
	"github.com/openimsdk/tools/utils/datautil"
)

func Secret(secret string) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		return []byte(secret), nil
	}
}

// XunLinkClaims 在上游 Claims 基础上增加租户字段（tenantID）。
// 向后兼容：上游 tools 的解析器会忽略多余字段；本包解析器可取出租户。
type XunLinkClaims struct {
	tokenverify.Claims
	TenantID int64 `json:"tenantID,omitempty"`
}

// BuildXunLinkClaims 生成带租户的 claims；租户取 userID 前缀（权威来源）
func BuildXunLinkClaims(uid string, platformID int, ttl int64) XunLinkClaims {
	tenantID, _ := tenantctx.ParseTenantFromUserID(uid)
	base := tokenverify.BuildClaims(uid, platformID, ttl)
	return XunLinkClaims{Claims: base, TenantID: tenantID}
}

func CheckAccessV3(ctx context.Context, ownerUserID string, imAdminUserID []string) (err error) {
	opUserID := mcontext.GetOpUserID(ctx)
	if datautil.Contain(opUserID, imAdminUserID...) {
		return nil
	}
	if opUserID == ownerUserID {
		return nil
	}
	return servererrs.ErrNoPermission.WrapMsg("ownerUserID", ownerUserID)
}

func IsAppManagerUid(ctx context.Context, imAdminUserID []string) bool {
	return datautil.Contain(mcontext.GetOpUserID(ctx), imAdminUserID...)
}

func CheckAdmin(ctx context.Context, imAdminUserID []string) error {
	if datautil.Contain(mcontext.GetOpUserID(ctx), imAdminUserID...) {
		return nil
	}
	return servererrs.ErrNoPermission.WrapMsg(fmt.Sprintf("user %s is not admin userID", mcontext.GetOpUserID(ctx)))
}

func IsManagerUserID(opUserID string, imAdminUserID []string) bool {
	return datautil.Contain(opUserID, imAdminUserID...)
}
