// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层/租户层集合模型（数据库字典 T-01~T-07/T-09/T-10，docs/dev/数据库字典.md）
// 代码引用示例：/* T-01 tenant */

package platform

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// T-01 tenant 租户
type Tenant struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	TenantID       int64              `bson:"tenant_id"` // 平台层固定 0（T-01 自身的数字租户 ID 见 TenantUID）
	TenantUID      int64              `bson:"tenant_uid"` // 数字租户ID（userID 前缀 t{tenant_uid}_ 的来源，全局递增）
	TenantCode     string             `bson:"tenant_code"`
	Name           string             `bson:"name"`
	Industry       string             `bson:"industry,omitempty"`
	PlanID         primitive.ObjectID `bson:"plan_id,omitempty"`
	SeatsPurchased int32              `bson:"seats_purchased"`
	Status         int32              `bson:"status"` // 0=待审核 1=正常 2=停用
	ExpireAt       time.Time          `bson:"expire_at,omitempty"`
	GuestWidgetOn  bool               `bson:"guest_widget_on"`
	Config         map[string]any     `bson:"config,omitempty"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
	DeletedAt      *time.Time         `bson:"deleted_at,omitempty"`
}

// T-02 plan 套餐
type Plan struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	Name           string             `bson:"name"`
	MaxMembers     int32              `bson:"max_members"`
	MaxSeats       int32              `bson:"max_seats"`
	MaxStorageGB   int32              `bson:"max_storage_gb"`
	MaxMonthlyMsg  int64              `bson:"max_monthly_msg"`
	Features       map[string]bool    `bson:"features"`
	PriceMonth     float64            `bson:"price_month,omitempty"`
	OnShelf        bool               `bson:"on_shelf"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
}

// T-03 platform_admin 总后台账号（独立账号体系）
type PlatformAdmin struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Username     string             `bson:"username"`
	PasswordHash string             `bson:"password_hash"`
	Role         string             `bson:"role"` // super / ops / readonly
	TotpOn       bool               `bson:"totp_on"`
	Status       int32              `bson:"status"` // 1=正常 0=禁用
	LastLoginAt  time.Time          `bson:"last_login_at,omitempty"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

// T-04 license 激活码/私有化授权（模型先行，签发流程 M3 实现）
type License struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	LicenseNo      string             `bson:"license_no"`
	MerchantName   string             `bson:"merchant_name"`
	TenantCode     string             `bson:"tenant_code,omitempty"`
	MachineFP      string             `bson:"machine_fp"`
	ApplyCode      string             `bson:"apply_code,omitempty"`
	ActivationCode string             `bson:"activation_code,omitempty"`
	NotBefore      int64              `bson:"not_before"`
	NotAfter       int64              `bson:"not_after"`
	GraceDays      int32              `bson:"grace_days"`
	Seats          int32              `bson:"seats"`
	Features       []string           `bson:"features"`
	Mode           string             `bson:"mode"` // online / offline
	Status         string             `bson:"status"`
	ActivatedAt    int64              `bson:"activated_at,omitempty"`
	LastHeartbeat  int64              `bson:"last_heartbeat,omitempty"`
	InstanceInfo   map[string]any     `bson:"instance_info,omitempty"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
}

// T-05 audit_log 操作审计（只写不删）
type AuditLog struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	TenantID  int64              `bson:"tenant_id"`
	ActorType string             `bson:"actor_type"` // platform / tenant / system
	ActorID   int64              `bson:"actor_id"`
	ActorName string             `bson:"actor_name"`
	Action    string             `bson:"action"`
	Target    string             `bson:"target,omitempty"`
	Detail    map[string]any     `bson:"detail,omitempty"`
	IP        string             `bson:"ip,omitempty"`
	CreatedAt time.Time          `bson:"created_at"`
}

// T-06 tenant_admin 商户后台账号
type TenantAdmin struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	TenantID     int64              `bson:"tenant_id"`
	Username     string             `bson:"username"`
	PasswordHash string             `bson:"password_hash"`
	Role         string             `bson:"role"` // tenant_admin / class_leader / agent
	Status       int32              `bson:"status"`
	LastLoginAt  time.Time          `bson:"last_login_at,omitempty"`
	CreatedAt    time.Time          `bson:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

// T-07 staff 客服/成员（IM 侧身份为 users 集合中的 t{tid}_u{uid}，本集合承载商户侧语义）
type Staff struct {
	ID          primitive.ObjectID `bson:"_id,omitempty"`
	TenantID    int64              `bson:"tenant_id"`
	IMUserID    string             `bson:"im_user_id"`
	Name        string             `bson:"name"`
	Mobile      string             `bson:"mobile,omitempty"`
	Role        string             `bson:"role"` // admin / class_leader / agent
	SeatOn      bool               `bson:"seat_on"`
	Status      int32              `bson:"status"` // 1=在职 0=禁用
	CreatedAt   time.Time          `bson:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at"`
}

// T-08 guest 匿名访客（M2 访客链路实现）
type Guest struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"`
	TenantID      int64              `bson:"tenant_id"`
	IMUserID      string             `bson:"im_user_id"`
	HistoryKey    string             `bson:"history_key"`
	Fingerprint   string             `bson:"fingerprint,omitempty"`
	SourceURL     string             `bson:"source_url,omitempty"`
	Status        int32              `bson:"status"` // 1=活跃 2=归档 3=拉黑
	LastActiveAt  time.Time          `bson:"last_active_at"`
	CreatedAt     time.Time          `bson:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"`
}

// T-09 agent_session 客服接待记录（M2 访客链路实现）
type AgentSession struct {
	ID             primitive.ObjectID `bson:"_id,omitempty"`
	TenantID       int64              `bson:"tenant_id"`
	GuestIMUserID  string             `bson:"guest_im_user_id"`
	AgentIMUserID  string             `bson:"agent_im_user_id,omitempty"`
	ConversationID string             `bson:"conversation_id"`
	SourceURL      string             `bson:"source_url,omitempty"`
	Status         string             `bson:"status"` // queueing/active/transferred/closed
	ClaimedAt      time.Time          `bson:"claimed_at,omitempty"`
	ClosedAt       time.Time          `bson:"closed_at,omitempty"`
	Rating         int32              `bson:"rating,omitempty"`
	CreatedAt      time.Time          `bson:"created_at"`
	UpdatedAt      time.Time          `bson:"updated_at"`
}

// T-10 tenant_quota_usage 配额用量日汇总
type QuotaUsage struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	TenantID     int64              `bson:"tenant_id"`
	Date         string             `bson:"date"` // YYYY-MM-DD
	Members      int32              `bson:"members"`
	OnlineAgents int32              `bson:"online_agents"`
	Messages     int64              `bson:"messages"`
	StorageBytes int64              `bson:"storage_bytes"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}
