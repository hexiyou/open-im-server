// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层后台任务：T-10 配额日汇总（每小时）+ 访客数据归档（每日）

package saasapi

import (
	"context"
	"log"
	"time"
)

// StartBackgroundTasks 启动后台任务（阻塞型 goroutine，随进程退出）
func (s *Server) StartBackgroundTasks() {
	go func() {
		for {
			s.runQuotaSnapshot()
			time.Sleep(time.Hour)
		}
	}()
	go func() {
		for {
			s.runGuestArchive()
			time.Sleep(24 * time.Hour)
		}
	}()
}

// runQuotaSnapshot 全量活跃租户的当日成员数/消息量快照（T-10）
func (s *Server) runQuotaSnapshot() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	uids, err := s.store.ListActiveTenantUIDs(ctx)
	if err != nil {
		log.Println("[xlink-saas] quota snapshot: list tenants failed:", err)
		return
	}
	now := time.Now()
	date := now.Format("2006-01-02")
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	for _, uid := range uids {
		members, err := s.store.CountStaffs(ctx, uid)
		if err != nil {
			continue
		}
		msgs, err := s.store.CountMessagesSince(ctx, uid, startOfDay)
		if err != nil {
			continue
		}
		if err := s.store.UpsertQuotaUsage(ctx, uid, date, int32(members), msgs); err != nil {
			log.Println("[xlink-saas] quota snapshot upsert failed, tenant:", uid, err)
		}
	}
}

// runGuestArchive 归档超期未活跃访客（T-08 保留期，M2 统一 30 天）
func (s *Server) runGuestArchive() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	n, err := s.store.ArchiveInactiveGuests(ctx, 30)
	if err != nil {
		log.Println("[xlink-saas] guest archive failed:", err)
		return
	}
	if n > 0 {
		log.Println("[xlink-saas] guest archived:", n)
	}
}
