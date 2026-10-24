// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层集合存储（MongoDB）。集合与索引在启动时创建（上游 mgo 同模式）。
// 审计插入失败不阻断业务（降级为日志告警）。

package platform

import (
	"context"
	"time"

	msgmodel "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
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
	CollGuestBlack    = "guest_blacklist"
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
		CollGuest: {
			mongo.IndexModel{Keys: bson.D{{Key: "im_user_id", Value: 1}}, Options: options.Index().SetUnique(true)},
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "last_active_at", Value: -1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "history_key", Value: 1}}},
		},
		CollGuestBlack: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "fingerprint", Value: 1}}, Options: options.Index().SetUnique(true)},
		},
		CollAgentSession: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "status", Value: 1}, {Key: "claimed_at", Value: -1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "conversation_id", Value: 1}}},
			mongo.IndexModel{Keys: bson.D{{Key: "agent_im_user_id", Value: 1}, {Key: "claimed_at", Value: -1}}},
		},
		CollQuotaUsage: {
			mongo.IndexModel{Keys: bson.D{{Key: "tenant_id", Value: 1}, {Key: "date", Value: 1}}, Options: options.Index().SetUnique(true)},
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

func (s *Store) GetTenantByUID(ctx context.Context, tenantUID int64) (*Tenant, error) {
	return mongoutil.FindOne[*Tenant](ctx, s.coll(CollTenant), bson.M{"tenant_uid": tenantUID, "deleted_at": nil})
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

// CountSeatOnStaffs 在职且占用席位的成员数（席位硬限制依据，错误码 40301）
func (s *Store) CountSeatOnStaffs(ctx context.Context, tenantID int64) (int64, error) {
	return mongoutil.Count(ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "status": 1, "seat_on": true})
}

// ListSeatOnStaffs 坐席池（坐席调度 idle_first 的候选集）
func (s *Store) ListSeatOnStaffs(ctx context.Context, tenantID int64) ([]*Staff, error) {
	return mongoutil.Find[*Staff](ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "status": 1, "seat_on": true})
}

func (s *Store) GetStaffByIMUserID(ctx context.Context, tenantID int64, imUserID string) (*Staff, error) {
	return mongoutil.FindOne[*Staff](ctx, s.coll(CollStaff), bson.M{"tenant_id": tenantID, "im_user_id": imUserID})
}

/* ---------------- T-08 guest ---------------- */

func (s *Store) CreateGuest(ctx context.Context, g *Guest) error {
	now := time.Now()
	g.CreatedAt, g.UpdatedAt, g.LastActiveAt = now, now, now
	return mongoutil.InsertMany(ctx, s.coll(CollGuest), []*Guest{g})
}

func (s *Store) GetGuestByHistoryKey(ctx context.Context, tenantID int64, historyKey string) (*Guest, error) {
	return mongoutil.FindOne[*Guest](ctx, s.coll(CollGuest), bson.M{"tenant_id": tenantID, "history_key": historyKey})
}

func (s *Store) TouchGuestActive(ctx context.Context, id primitive.ObjectID) error {
	return mongoutil.UpdateOne(ctx, s.coll(CollGuest), bson.M{"_id": id},
		bson.M{"$set": bson.M{"last_active_at": time.Now(), "status": 1}}, false)
}

func (s *Store) GetGuestFingerprintBlocked(ctx context.Context, tenantID int64, fingerprint string) (bool, error) {
	if fingerprint == "" {
		return false, nil
	}
	return mongoutil.Exist(ctx, s.coll(CollGuest), bson.M{"tenant_id": tenantID, "fingerprint": fingerprint, "status": 3})
}

/* ---------------- T-09 agent_session ---------------- */

func (s *Store) CreateAgentSession(ctx context.Context, a *AgentSession) error {
	now := time.Now()
	a.CreatedAt, a.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollAgentSession), []*AgentSession{a})
}

// CountActiveByAgent 坐席当前接待中会话数（调度权重）
func (s *Store) CountActiveByAgent(ctx context.Context, tenantID int64, agentIMUserID string) (int64, error) {
	return mongoutil.Count(ctx, s.coll(CollAgentSession),
		bson.M{"tenant_id": tenantID, "agent_im_user_id": agentIMUserID, "status": bson.M{"$in": []string{"active", "queueing"}}})
}

// PickAgent 坐席调度（idle_first）：在候选坐席（在线名单∩坐席池）中选当前接待数最少者；
// online 传 nil 表示不过滤在线态（Redis 不可用降级）；无候选返回 ErrNoAgentAvailable
func (s *Store) PickAgent(ctx context.Context, tenantID int64, online map[string]bool) (*Staff, error) {
	pool, err := s.ListSeatOnStaffs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	var best *Staff
	bestLoad := int64(-1)
	for _, st := range pool {
		if online != nil && !online[st.IMUserID] {
			continue
		}
		load, err := s.CountActiveByAgent(ctx, tenantID, st.IMUserID)
		if err != nil {
			return nil, err
		}
		if best == nil || load < bestLoad {
			best, bestLoad = st, load
		}
	}
	if best == nil {
		return nil, ErrNoAgentAvailable
	}
	return best, nil
}

var ErrNoAgentAvailable = errs.NewCodeError(40402, "no agent available")

/* ---------------- T-09 接待统计（配额写入侧） ---------------- */

func (s *Store) UpsertQuotaUsage(ctx context.Context, tenantID int64, date string, members int32, messages int64) error {
	_, err := s.coll(CollQuotaUsage).UpdateOne(ctx,
		bson.M{"tenant_id": tenantID, "date": date},
		bson.M{"$set": bson.M{"members": members, "messages": messages, "updated_at": time.Now()},
			"$setOnInsert": bson.M{"online_agents": int32(0), "storage_bytes": int64(0)}},
		options.Update().SetUpsert(true))
	return errs.Wrap(err)
}

/* ---------------- T-09 会话操作（坐席工作台） ---------------- */

func (s *Store) GetAgentSession(ctx context.Context, tenantID int64, id primitive.ObjectID) (*AgentSession, error) {
	return mongoutil.FindOne[*AgentSession](ctx, s.coll(CollAgentSession), bson.M{"tenant_id": tenantID, "_id": id})
}

func (s *Store) UpdateAgentSession(ctx context.Context, tenantID int64, id primitive.ObjectID, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	return mongoutil.UpdateOne(ctx, s.coll(CollAgentSession), bson.M{"tenant_id": tenantID, "_id": id}, bson.M{"$set": updates}, false)
}

// ListSessions status 为空则不过滤
func (s *Store) ListSessions(ctx context.Context, tenantID int64, status, agentIMUserID string, page Page) (int64, []*AgentSession, error) {
	filter := bson.M{"tenant_id": tenantID}
	if status != "" {
		filter["status"] = status
	}
	if agentIMUserID != "" {
		filter["agent_im_user_id"] = agentIMUserID
	}
	return mongoutil.FindPage[*AgentSession](ctx, s.coll(CollAgentSession), filter, page,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

// ListQueueingSessions 排队中会话（认领队列）
func (s *Store) ListQueueingSessions(ctx context.Context, tenantID int64) ([]*AgentSession, error) {
	return mongoutil.Find[*AgentSession](ctx, s.coll(CollAgentSession),
		bson.M{"tenant_id": tenantID, "status": "queueing"}, options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
}

// GetLatestSessionByGuest 访客最近一次接待（满意度评价用）
func (s *Store) GetLatestSessionByGuest(ctx context.Context, tenantID int64, guestIMUserID string) (*AgentSession, error) {
	return mongoutil.FindOne[*AgentSession](ctx, s.coll(CollAgentSession),
		bson.M{"tenant_id": tenantID, "guest_im_user_id": guestIMUserID},
		options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

// CountMessagesSince 统计租户自 startUnix（秒）以来的消息量（T-10 汇总用）
func (s *Store) CountMessagesSince(ctx context.Context, tenantID int64, startUnix int64) (int64, error) {
	type countRes struct {
		N int64 `bson:"n"`
	}
	match := bson.M{"msgs.msg.send_time": bson.M{"$gte": startUnix}}
	if tenantID > 0 {
		match["msgs.tenant_id"] = tenantID // 0 = 全平台（metrics 汇总用）
	}
	pipeline := bson.A{
		bson.M{"$unwind": "$msgs"},
		bson.M{"$match": match},
		bson.M{"$count": "n"},
	}
	items, err := mongoutil.Aggregate[countRes](ctx, s.DB.Collection("msg"), pipeline)
	if err != nil {
		return 0, errs.Wrap(err)
	}
	if len(items) == 0 {
		return 0, nil
	}
	return items[0].N, nil
}

// CountDocs 通用集合计数（metrics 汇总用）
func (s *Store) CountDocs(ctx context.Context, collection string, filter bson.M) (int64, error) {
	return mongoutil.Count(ctx, s.coll(collection), filter)
}

// GetMessagesByConversation 按会话ID拉取消息（doc_id 前缀匹配 → 逐条过滤租户），按 seq 升序
func (s *Store) GetMessagesByConversation(ctx context.Context, tenantID int64, conversationID string, limit int) ([]*msgmodel.MsgInfoModel, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	docs, err := mongoutil.Find[*msgmodel.MsgDocModel](ctx, s.DB.Collection("msg"),
		bson.M{"doc_id": primitive.Regex{Pattern: "^" + conversationID + ":"}})
	if err != nil {
		return nil, err
	}
	var out []*msgmodel.MsgInfoModel
	for _, doc := range docs {
		for _, m := range doc.Msg {
			if m != nil && m.TenantID == tenantID {
				out = append(out, m)
			}
		}
	}
	// 按 seq 升序，截取最近 limit 条
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Msg != nil && out[i].Msg != nil && out[j].Msg.Seq < out[i].Msg.Seq {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// ArchiveInactiveGuests 归档超过 retainDays 无活动的访客，返回归档数量
func (s *Store) ArchiveInactiveGuests(ctx context.Context, retainDays int) (int64, error) {
	if retainDays <= 0 {
		retainDays = 30
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	res, err := s.coll(CollGuest).UpdateMany(ctx,
		bson.M{"status": 1, "last_active_at": bson.M{"$lt": cutoff}},
		bson.M{"$set": bson.M{"status": 2, "updated_at": time.Now()}})
	if err != nil {
		return 0, errs.Wrap(err)
	}
	return res.ModifiedCount, nil
}

// ListActiveTenantUIDs 所有正常状态的租户UID（后台任务遍历用）
func (s *Store) ListActiveTenantUIDs(ctx context.Context) ([]int64, error) {
	tenants, err := mongoutil.Find[*Tenant](ctx, s.coll(CollTenant), bson.M{"status": 1, "deleted_at": nil})
	if err != nil {
		return nil, err
	}
	uids := make([]int64, 0, len(tenants))
	for _, t := range tenants {
		uids = append(uids, t.TenantUID)
	}
	return uids, nil
}

/* ---------------- T-04 license（一机一码授权，M3） ---------------- */

func (s *Store) CreateLicense(ctx context.Context, l *License) error {
	now := time.Now()
	l.CreatedAt, l.UpdatedAt = now, now
	return mongoutil.InsertMany(ctx, s.coll(CollLicense), []*License{l})
}

func (s *Store) GetLicenseByID(ctx context.Context, id primitive.ObjectID) (*License, error) {
	return mongoutil.FindOne[*License](ctx, s.coll(CollLicense), bson.M{"_id": id})
}

func (s *Store) GetLicenseByNoAndFP(ctx context.Context, licenseNo, machineFP string) (*License, error) {
	return mongoutil.FindOne[*License](ctx, s.coll(CollLicense), bson.M{"license_no": licenseNo, "machine_fp": machineFP})
}

func (s *Store) ListLicenses(ctx context.Context, status string, page Page) (int64, []*License, error) {
	filter := bson.M{}
	if status != "" {
		filter["status"] = status
	}
	return mongoutil.FindPage[*License](ctx, s.coll(CollLicense), filter, page,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

func (s *Store) UpdateLicense(ctx context.Context, id primitive.ObjectID, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	return mongoutil.UpdateOne(ctx, s.coll(CollLicense), bson.M{"_id": id}, bson.M{"$set": updates}, false)
}

/* ---------------- T-08b 访客黑名单 ---------------- */

func (s *Store) ListGuestBlacklist(ctx context.Context, tenantID int64) ([]*GuestBlacklist, error) {
	return mongoutil.Find[*GuestBlacklist](ctx, s.coll(CollGuestBlack),
		bson.M{"tenant_id": tenantID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
}

func (s *Store) AddGuestBlacklist(ctx context.Context, tenantID int64, fingerprint, note string) error {
	b := &GuestBlacklist{TenantID: tenantID, Fingerprint: fingerprint, Note: note, CreatedAt: time.Now()}
	return mongoutil.InsertMany(ctx, s.coll(CollGuestBlack), []*GuestBlacklist{b})
}

func (s *Store) RemoveGuestBlacklist(ctx context.Context, tenantID int64, id primitive.ObjectID) error {
	return mongoutil.DeleteOne(ctx, s.coll(CollGuestBlack), bson.M{"tenant_id": tenantID, "_id": id})
}

func (s *Store) IsGuestBlacklisted(ctx context.Context, tenantID int64, fingerprint string) (bool, error) {
	if fingerprint == "" {
		return false, nil
	}
	return mongoutil.Exist(ctx, s.coll(CollGuestBlack), bson.M{"tenant_id": tenantID, "fingerprint": fingerprint})
}

// UpdateTenantConfig 合并更新租户 config JSON（商户后台访客接入设置等）
func (s *Store) UpdateTenantConfig(ctx context.Context, tenantUID int64, merge map[string]any) error {
	t, err := s.GetTenantByUID(ctx, tenantUID)
	if err != nil {
		return err
	}
	cfg := t.Config
	if cfg == nil {
		cfg = map[string]any{}
	}
	for k, v := range merge {
		cfg[k] = v
	}
	return s.UpdateTenant(ctx, t.ID, bson.M{"config": cfg})
}

// ListQuotaUsage 近 N 天日汇总（报表用）
func (s *Store) ListQuotaUsage(ctx context.Context, tenantID int64, limit int) ([]*QuotaUsage, error) {
	if limit <= 0 || limit > 90 {
		limit = 14
	}
	return mongoutil.Find[*QuotaUsage](ctx, s.coll(CollQuotaUsage),
		bson.M{"tenant_id": tenantID}, options.Find().SetSort(bson.D{{Key: "date", Value: -1}}).SetLimit(int64(limit)))
}

/* ---------------- T-06 租户管理员扩展 ---------------- */

func (s *Store) ListTenantAdmins(ctx context.Context, tenantID int64) ([]*TenantAdmin, error) {
	return mongoutil.Find[*TenantAdmin](ctx, s.coll(CollTenantAdmin), bson.M{"tenant_id": tenantID})
}

func (s *Store) UpdateTenantAdminByUsernames(ctx context.Context, tenantID int64, usernames []string, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	filter := bson.M{"tenant_id": tenantID}
	if len(usernames) > 0 {
		filter["username"] = bson.M{"$in": usernames}
	}
	_, err := mongoutil.UpdateMany(ctx, s.coll(CollTenantAdmin), filter, bson.M{"$set": updates})
	return errs.Wrap(err)
}

/* ---------------- T-03 平台管理员管理（API-PLAT-16） ---------------- */

func (s *Store) ListPlatformAdmins(ctx context.Context, page Page) (int64, []*PlatformAdmin, error) {
	return mongoutil.FindPage[*PlatformAdmin](ctx, s.coll(CollPlatformAdmin), bson.M{}, page,
		options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}}))
}

func (s *Store) UpdatePlatformAdmin(ctx context.Context, id primitive.ObjectID, updates bson.M) error {
	if updates == nil {
		updates = bson.M{}
	}
	updates["updated_at"] = time.Now()
	return mongoutil.UpdateOne(ctx, s.coll(CollPlatformAdmin), bson.M{"_id": id}, bson.M{"$set": updates}, false)
}
