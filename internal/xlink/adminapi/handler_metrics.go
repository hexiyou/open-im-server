// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 总后台工作台指标聚合（API-PLAT-13 overview）

package adminapi

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/license"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/openimsdk/tools/apiresp"
	"go.mongodb.org/mongo-driver/bson"
)

func (s *Server) metricsOverview(c *gin.Context) {
	ctx := c.Request.Context()
	tenantsTotal, _ := s.store.CountDocs(ctx, platform.CollTenant, bson.M{"deleted_at": nil})
	tenantsActive, _ := s.store.CountDocs(ctx, platform.CollTenant, bson.M{"deleted_at": nil, "status": 1})
	licensesTotal, _ := s.store.CountDocs(ctx, platform.CollLicense, bson.M{})
	licensesActivated, _ := s.store.CountDocs(ctx, platform.CollLicense, bson.M{"status": bson.M{"$in": []string{license.StatusActivated, license.StatusGrace}}})
	licensesRevoked, _ := s.store.CountDocs(ctx, platform.CollLicense, bson.M{"status": license.StatusRevoked})
	staffTotal, _ := s.store.CountDocs(ctx, platform.CollStaff, bson.M{"status": 1})
	guestTotal, _ := s.store.CountDocs(ctx, platform.CollGuest, bson.M{})
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	messagesToday, _ := s.store.CountMessagesSince(ctx, 0, startOfDay)
	sessionsActive, _ := s.store.CountDocs(ctx, platform.CollAgentSession, bson.M{"status": "active"})

	apiresp.GinSuccess(c, gin.H{
		"tenants":   gin.H{"total": tenantsTotal, "active": tenantsActive, "disabled": tenantsTotal - tenantsActive},
		"licenses":  gin.H{"total": licensesTotal, "activated": licensesActivated, "revoked": licensesRevoked},
		"staffTotal": staffTotal,
		"guestTotal": guestTotal,
		"messagesToday": messagesToday,
		"sessionsActive": sessionsActive,
	})
}
