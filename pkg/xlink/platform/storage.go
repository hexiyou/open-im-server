// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层集合存储（MongoDB）。集合与索引在启动时创建（上游 mgo 同模式）。
// 审计插入失败不阻断业务（降级为日志告警）。

package platform

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/xlink/tenantctx"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 集合名（与数据库字典一致）
const (
	CollTenant        = "tenant"
	CollPlan          = "plan"
	CollPlatformAdmin = "platform_admin"
	CollLicense       = "license"
	CollAuditLog      = "audit_log"
	CollTenantAdmin   = "tenant_admin"
	CollStaff         = "staff"
	CollGuest         = "guest"
	CollAgentSession  = "agent_session"
	CollQuotaUsage    = "tenant_quota_usage"
)

// Page 实现 tools 的 pagination.Pagination 接口
type Page struct {
	PageNumber int32
	ShowNumber int32
}

func (p Page) GetPageNumber() int32  { return p.PageNumber }
func (p Page) GetShowNumber() int32  { return p.ShowNumber }

type Store struct {
	DB *mongo.Database
}

func NewStore(ctx context.Context, db *mongo.Database) (*Store, error) {
	s := &Store{DB: db}
	if err := s.ensureIndexes(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) coll(name string) *mongo.Collection { return s.DB.Collection(name) }

func (s *Store) ensureIndexes(ctx context.Context) error {
	indexes := map[string][]mongo.IndexModel{
		CollTenant: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_code", Value: 1}}, Options: options.Index().SetUnique(true)},
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_uid", Value: 1}}, Options: options.Index().SetUnique(true)},
			mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}, {Key: "plan_id", Value: 1}}},
		},
		CollPlan: {
			mongo.IndexModel{Keys: bson.D{{Key: "on_shelf", Value: 1}}},
		},
		CollPlatformAdmin: {
			mongo.IndexModel{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		CollLicense: {
			mongo.IndexModel{Keys: bson.D{{Key: "license_no", Value: 1}}, Options: options.Index().SetUnique(true)},
			mongo.IndexModel{Keys: bson.D{{Key: "machine_fp", Value: 1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "status", Value: 1}}},
		},
		CollAuditLog: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "created_at", Value: -1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "actor_id", Value: 1}, {Key: "created_at", Value: -1}}},
		},
		CollTenantAdmin: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		CollStaff: {
			mongo.IndexModel{Keys: bson.D{{Key: "im_user_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}}},
		},
	}
	for name, models := range indexes {
		if _, err := s.coll(name).Indexes().CreateMany(ctx, models); err != nil {
			return errs.WrapMsg(err, "ensure index failed", "collection", name)
		}
	}
	return nil
}

/* ---------------- T-01 tenant ---------------- */

// CreateTenant 创建租户；tenant_uid 取全局递增（在 tenant 集合上用计数文档/高水位实现）
func (s *Store) CreateTenant(ctx context.Context, t *Tenant) error {
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	// 高水位：取当前最大 tenant_uid + 1（平台层写入频度低，简单实现足够；并发场景由唯一索引兜底重试）
	var last Tenant
	err := s.coll(CollTenant).FindOne(ctx, bson.M{}, options.FindOne().SetSort(bson.D{{Key: "tenant_uid", Value: -1}})).Decode(&last)
	if err == nil {
		t.TenantUID = last.TenantUID + 1
	} else if errs.Unwrap(err) == mongo.ErrNoDocuments {
		t.TenantUID = 1
	} else {
		return errs.Wrap(err)
	}
	return mongoutil.InsertMany(ctx, s.coll(CollTenant), []*Tenant{t})
}

func (s *Store) GetTenantByCode(ctx context.Context, code string) (*Tenant, error) {
	return mongoutil.FindOne[*Tenant](ctx, s.coll(CollTenant), bson.M{"tenant_code": code, "deleted_at": nil})
}

func (s *Store) GetTenantByID(ctx context.Context, id primitive.ObjectID) (*Tenant, error) {
	return mongoutil.FindOne[*Tenant](ctx, s.coll(CollTenant), bson.M{"_id": id, "deleted_at": nil})
}

// ListTenants status<0 表示不过滤；keyword 匹配 name/tenant_code 前缀
func (s *Store) ListTenants(ctx context.Context, status int32, keyword string, page Page) (int64, []*Tenant, error) {
	filter := bson.M{"deleted_at": nil}
	if status >= 0 {
		filter["status"] = status
	}
	if keyword != "" {
		filter["$or"] = []bson.M{
			{"name": primitive.Regex{Pattern: keyword, Options: "i"}},
			{"tenant_code": primitive.Regex{Pattern: keyword, Options: "i"}},
		}
	}
	return mongoutil.FindPage[*Tenant](ctx, s.coll(CollTenant), filter, page, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

func (s *Store) UpdateTenant(ctx context.Context, id primitive.ObjectID, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	return mongoutil.UpdateOne(ctx, s.coll(CollTenant), bson.M{"_id": id}, bson.M{"$set": updates}, false)
}

func (s *Store) CountTenants(ctx context.Context) (int64, error) {
	return mongoutil.Count(ctx, s.coll(CollTenant), bson.M{"deleted_at": nil})
}

/* ---------------- T-02 plan ---------------- */

func (s *Store) CreatePlan(ctx context.Context, p *Plan) error {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollPlan), []*Plan{p})
}

func (s *Store) ListPlans(ctx context.Context, onShelfOnly bool) ([]*Plan, error) {
	filter := bson.M{}
	if onShelfOnly {
		filter["on_shelf"] = true
	}
	return mongoutil.Find[*Plan](ctx, s.coll(CollPlan), filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
}

func (s *Store) GetPlan(ctx context.Context, id primitive.ObjectID) (*Plan, error) {
	return mongoutil.FindOne[*Plan](ctx, s.coll(CollPlan), bson.M{"_id": id})
}

/* ---------------- T-03 platform_admin ---------------- */

func (s *Store) CountPlatformAdmins(ctx context.Context) (int64, error) {
	return mongoutil.Count(ctx, s.coll(CollPlatformAdmin), bson.M{})
}

func (s *Store) CreatePlatformAdmin(ctx context.Context, a *PlatformAdmin) error {
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollPlatformAdmin), []*PlatformAdmin{a})
}

func (s *Store) GetPlatformAdminByUsername(ctx context.Context, username string) (*PlatformAdmin, error) {
	return mongoutil.FindOne[*PlatformAdmin](ctx, s.coll(CollPlatformAdmin), bson.M{"username": username})
}

func (s *Store) TouchPlatformAdminLogin(ctx context.Context, id primitive.ObjectID) error {
	return mongoutil.UpdateOne(ctx, s.coll(CollPlatformAdmin), bson.M{"_id": id}, bson.M{"$set": bson.M{"last_login_at": time.Now()}}, false)
}

/* ---------------- T-05 audit_log ---------------- */

func (s *Store) InsertAudit(ctx context.Context, a *AuditLog) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	if a.TenantID == 0 {
		a.TenantID = tenantctx.GetTenantID(ctx)
	}
	return mongoutil.InsertMany(ctx, s.coll(CollAuditLog), []*AuditLog{a})
}

func (s *Store) ListAudits(ctx context.Context, tenantID int64, actorType, action string, page Page) (int64, []*AuditLog, error) {
	filter := bson.M{}
	if tenantID >= 0 {
		filter["tenant_id"] = tenantID
	}
	if actorType != "" {
		filter["actor_type"] = actorType
	}
	if action != "" {
		filter["action"] = action
	}
	return mongoutil.FindPage[*AuditLog](ctx, s.coll(CollAuditLog), filter, page, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

/* ---------------- T-06 tenant_admin ---------------- */

func (s *Store) CreateTenantAdmin(ctx context.Context, a *TenantAdmin) error {
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollTenantAdmin), []*TenantAdmin{a})
}

func (s *Store) GetTenantAdmin(ctx context.Context, tenantID int64, username string) (*TenantAdmin, error) {
	return mongoutil.FindOne[*TenantAdmin](ctx, s.coll(CollTenantAdmin), bson.M{"tenant_id": tenantID, "username": username})
}

func (s *Store) TouchTenantAdminLogin(ctx context.Context, id primitive.ObjectID) error {
	return mongoutil.UpdateOne(ctx, s.coll(CollTenantAdmin), bson.M{"_id": id}, bson.M{"$set": bson.M{"last_login_at": time.Now()}}, false)
}

/* ---------------- T-07 staff ---------------- */

func (s *Store) CreateStaff(ctx context.Context, st *Staff) error {
	now := time.Now()
	st.CreatedAt, st.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollStaff), []*Staff{st})
}

func (s *Store) ListStaffs(ctx context.Context, tenantID int64, page Page) (int64, []*Staff, error) {
	filter := bson.M{"tenant_id": tenantID}
	return mongoutil.FindPage[*Staff](ctx, s.coll(CollStaff), filter, page, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

func (s *Store) GetStaff(ctx context.Context, tenantID int64, id primitive.ObjectID) (*Staff, error) {
	return mongoutil.FindOne[*Staff](ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "_id": id})
}

func (s *Store) UpdateStaff(ctx context.Context, tenantID int64, id primitive.ObjectID, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	return mongoutil.UpdateOne(ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "_id": id}, bson.M{"$set": updates}, false)
}

func (s *Store) CountStaffs(ctx context.Context, tenantID int64) (int64, error) {
	return mongoutil.Count(ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "status": 1})
}
