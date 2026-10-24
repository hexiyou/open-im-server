// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层/租户层集合模型（数据库字典 T-01~T-07/T-09/T-10，docs/dev/数据库字典.md）
// 代码引用示例：/* T-01 tenant */
// JSON 输出约定：lowerCamel；密码哈希/敏感字段一律 json:"-" 永不出接口

package platform

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// T-01 tenant 租户
type Tenant struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID       int64              `bson:"tenant_id" json:"tenantID"`
	TenantUID      int64              `bson:"tenant_uid" json:"tenantUID"`
	TenantCode     string             `bson:"tenant_code" json:"tenantCode"`
	Name           string             `bson:"name" json:"name"`
	Industry       string             `bson:"industry,omitempty" json:"industry,omitempty"`
	PlanID         primitive.ObjectID `bson:"plan_id,omitempty" json:"planID,omitempty"`
	SeatsPurchased int32              `bson:"seats_purchased" json:"seatsPurchased"`
	Status         int32              `bson:"status" json:"status"` // 0=待审核 1=正常 2=停用
	ExpireAt       time.Time          `bson:"expire_at,omitempty" json:"expireAt,omitempty"`
	GuestWidgetOn  bool               `bson:"guest_widget_on" json:"guestWidgetOn"`
	Config         map[string]any     `bson:"config,omitempty" json:"config,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
	DeletedAt      *time.Time         `bson:"deleted_at,omitempty" json:"deletedAt,omitempty"`
}

// T-02 plan 套餐
type Plan struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Name           string             `bson:"name" json:"name"`
	MaxMembers     int32              `bson:"max_members" json:"maxMembers"`
	MaxSeats       int32              `bson:"max_seats" json:"maxSeats"`
	MaxStorageGB   int32              `bson:"max_storage_gb" json:"maxStorageGB"`
	MaxMonthlyMsg  int64              `bson:"max_monthly_msg" json:"maxMonthlyMsg"`
	Features       map[string]bool    `bson:"features" json:"features"`
	PriceMonth     float64            `bson:"price_month,omitempty" json:"priceMonth,omitempty"`
	OnShelf        bool               `bson:"on_shelf" json:"onShelf"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-03 platform_admin 总后台账号（独立账号体系）
type PlatformAdmin struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Username     string             `bson:"username" json:"username"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Role         string             `bson:"role" json:"role"` // super / ops / readonly
	TotpOn       bool               `bson:"totp_on" json:"totpOn"`
	Status       int32              `bson:"status" json:"status"` // 1=正常 0=禁用
	LastLoginAt  time.Time          `bson:"last_login_at,omitempty" json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-04 license 激活码/私有化授权（模型先行，签发流程 M3 实现）
type License struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	LicenseNo      string             `bson:"license_no" json:"licenseNo"`
	MerchantName   string             `bson:"merchant_name" json:"merchantName"`
	TenantCode     string             `bson:"tenant_code,omitempty" json:"tenantCode,omitempty"`
	MachineFP      string             `bson:"machine_fp" json:"machineFP"`
	ApplyCode      string             `bson:"apply_code,omitempty" json:"-"`
	ActivationCode string             `bson:"activation_code,omitempty" json:"-"` // 永不出列表接口
	NotBefore      int64              `bson:"not_before" json:"notBefore"`
	NotAfter       int64              `bson:"not_after" json:"notAfter"`
	GraceDays      int32              `bson:"grace_days" json:"graceDays"`
	Seats          int32              `bson:"seats" json:"seats"`
	Features       []string           `bson:"features" json:"features"`
	Mode           string             `bson:"mode" json:"mode"` // online / offline
	Status         string             `bson:"status" json:"status"`
	ActivatedAt    int64              `bson:"activated_at,omitempty" json:"activatedAt,omitempty"`
	LastHeartbeat  int64              `bson:"last_heartbeat,omitempty" json:"lastHeartbeat,omitempty"`
	InstanceInfo   map[string]any     `bson:"instance_info,omitempty" json:"instanceInfo,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-05 audit_log 操作审计（只写不删）
type AuditLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID  int64              `bson:"tenant_id" json:"tenantID"`
	ActorType string             `bson:"actor_type" json:"actorType"` // platform / tenant / system
	ActorID   int64              `bson:"actor_id" json:"actorID"`
	ActorName string             `bson:"actor_name" json:"actorName"`
	Action    string             `bson:"action" json:"action"`
	Target    string             `bson:"target,omitempty" json:"target,omitempty"`
	Detail    map[string]any     `bson:"detail,omitempty" json:"detail,omitempty"`
	IP        string             `bson:"ip,omitempty" json:"ip,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"createdAt"`
}

// T-06 tenant_admin 商户后台账号
type TenantAdmin struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID     int64              `bson:"tenant_id" json:"tenantID"`
	Username     string             `bson:"username" json:"username"`
	PasswordHash string             `bson:"password_hash" json:"-"`
	Role         string             `bson:"role" json:"role"` // tenant_admin / class_leader / agent
	Status       int32              `bson:"status" json:"status"`
	LastLoginAt  time.Time          `bson:"last_login_at,omitempty" json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-07 staff 客服/成员（IM 侧身份为 users 集合中的 t{tid}_u{uid}，本集合承载商户侧语义）
type Staff struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID     int64              `bson:"tenant_id" json:"tenantID"`
	IMUserID     string             `bson:"im_user_id" json:"imUserID"`
	Name         string             `bson:"name" json:"name"`
	Mobile       string             `bson:"mobile,omitempty" json:"mobile,omitempty"`
	Role         string             `bson:"role" json:"role"` // admin / class_leader / agent
	SeatOn       bool               `bson:"seat_on" json:"seatOn"`
	PasswordHash string             `bson:"password_hash,omitempty" json:"-"` // 初始密码 bcrypt（/c/auth/login 凭据），永不出接口
	Status       int32              `bson:"status" json:"status"`             // 1=在职 0=禁用
	CreatedAt    time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-08 guest 匿名访客（M2 访客链路实现）
type Guest struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID      int64              `bson:"tenant_id" json:"tenantID"`
	IMUserID      string             `bson:"im_user_id" json:"imUserID"`
	HistoryKey    string             `bson:"history_key" json:"historyKey"`
	Fingerprint   string             `bson:"fingerprint,omitempty" json:"fingerprint,omitempty"`
	SourceURL     string             `bson:"source_url,omitempty" json:"sourceURL,omitempty"`
	Status        int32              `bson:"status" json:"status"` // 1=活跃 2=归档 3=拉黑
	LastActiveAt  time.Time          `bson:"last_active_at" json:"lastActiveAt"`
	CreatedAt     time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt     time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-09 agent_session 客服接待记录（M2 访客链路实现）
type AgentSession struct {
	ID             primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID       int64              `bson:"tenant_id" json:"tenantID"`
	GuestIMUserID  string             `bson:"guest_im_user_id" json:"guestIMUserID"`
	AgentIMUserID  string             `bson:"agent_im_user_id,omitempty" json:"agentIMUserID,omitempty"`
	ConversationID string             `bson:"conversation_id" json:"conversationID"`
	SourceURL      string             `bson:"source_url,omitempty" json:"sourceURL,omitempty"`
	Status         string             `bson:"status" json:"status"` // queueing/active/transferred/closed
	ClaimedAt      time.Time          `bson:"claimed_at,omitempty" json:"claimedAt,omitempty"`
	ClosedAt       time.Time          `bson:"closed_at,omitempty" json:"closedAt,omitempty"`
	Rating         int32              `bson:"rating,omitempty" json:"rating,omitempty"`
	CreatedAt      time.Time          `bson:"created_at" json:"createdAt"`
	UpdatedAt      time.Time          `bson:"updated_at" json:"updatedAt"`
}

// T-10 tenant_quota_usage 配额用量日汇总
type QuotaUsage struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	TenantID     int64              `bson:"tenant_id" json:"tenantID"`
	Date         string             `bson:"date" json:"date"` // YYYY-MM-DD
	Members      int32              `bson:"members" json:"members"`
	OnlineAgents int32              `bson:"online_agents" json:"onlineAgents"`
	Messages     int64              `bson:"messages" json:"messages"`
	StorageBytes int64              `bson:"storage_bytes" json:"storageBytes"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updatedAt"`
}
