// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 授权门禁：私有化部署（license.enabled=true）时对业务接口执行降级检查；
// SaaS 平台侧（enabled=false）恒放行。

package platform

import (
	"sync"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/xlink/license"
)

type LicenseGate struct {
	enabled   bool
	localDir  string
	mu        sync.RWMutex
	status    string
	checkErr  error
	lastCheck time.Time
}

func NewLicenseGate(enabled bool, localDir string) *LicenseGate {
	return &LicenseGate{enabled: enabled, localDir: localDir, status: license.StatusUnactivated}
}

func (g *LicenseGate) Enabled() bool { return g.enabled }

// Check 返回当前状态（10s 缓存）；未启用恒放行。
// 语义（docs/02 §4.4）：activated/grace → nil（宽限期正常运行业务，状态供 /license/status 提示）；
// degraded/revoked/unactivated → 45101 拒绝
func (g *LicenseGate) Check() (string, error) {
	if !g.enabled {
		return license.StatusActivated, nil
	}
	g.mu.RLock()
	if time.Since(g.lastCheck) < 10*time.Second {
		st, err := g.status, g.checkErr
		g.mu.RUnlock()
		return st, err
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	_, status, err := license.CurrentState(g.localDir)
	g.checkErr = err
	if status == license.StatusGrace {
		err = nil // 宽限期放行业务
	}
	g.status, g.lastCheck = status, time.Now()
	return status, err
}
