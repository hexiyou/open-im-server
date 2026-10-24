// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 心跳 agent（在线模式）：定期向平台校验服务上报指纹与授权号，
// 平台可远程吊销/判过期；结果回写本地状态。离线模式不启用心跳。

package license

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// HeartbeatRequest API-SYS-01 请求体
type HeartbeatRequest struct {
	MachineFP string `json:"machineFP"`
	LicenseNo string `json:"licenseNo"`
	Version   string `json:"version,omitempty"`
}

// HeartbeatResponse API-SYS-01 响应体
type HeartbeatResponse struct {
	OK             bool   `json:"ok"`
	Status         string `json:"status"`                   // 平台侧状态（activated/grace/degraded/revoked）
	Reason         string `json:"reason,omitempty"`
	GraceRemaining int    `json:"graceRemainingDays,omitempty"`
}

// StartHeartbeatLoop 启动心跳循环（阻塞 goroutine）：
// platformURL 平台校验服务基地址；localDir 本地授权目录；interval 心跳间隔；
// onChange 状态变化回调（服务据此启用/解除降级）
func StartHeartbeatLoop(platformURL, localDir, version string, interval time.Duration, onChange func(newState LocalState)) {
	go func() {
		runOnce := func() {
			ll, err := LoadLocal(localDir)
			if err != nil || ll == nil || ll.Payload == nil {
				return // 未激活，静默等待激活
			}
			reqBody, _ := json.Marshal(HeartbeatRequest{
				MachineFP: ll.Payload.MachineFP,
				LicenseNo: ll.Payload.LicenseID,
				Version:   version,
			})
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Post(platformURL+"/internal/license/heartbeat", "application/json", bytes.NewReader(reqBody))
			if err != nil {
				log.Println("[xlink-license] heartbeat unreachable:", err)
				return // 保持本地状态（宽限计时自然推进）
			}
			defer resp.Body.Close()
			// 响应为平台统一包装 {errCode, msg, data}，需解包 data 层
			var wrapped struct {
				ErrCode int              `json:"errCode"`
				Data    HeartbeatResponse `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
				log.Println("[xlink-license] heartbeat decode failed:", err)
				return
			}
			hb := wrapped.Data
			if wrapped.ErrCode != 0 {
				// 非 200 语义（如授权号不存在）：按心跳失败处理，保持宽限计时
				log.Println("[xlink-license] heartbeat rejected:", wrapped.ErrCode)
				return
			}
			if !hb.OK {
				ll.State.Status = StatusRevoked
				if hb.Status == StatusDegraded {
					ll.State.Status = StatusDegraded
				}
				ll.State.Reason = hb.Reason
			} else {
				ll.State.Status = StatusActivated
				ll.State.LastOKHB = time.Now().Unix()
				ll.State.Reason = ""
			}
			if err := SaveLocal(localDir, ll); err != nil {
				log.Println("[xlink-license] save local state failed:", err)
				return
			}
			if onChange != nil {
				onChange(ll.State)
			}
		}
		runOnce()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			runOnce()
		}
	}()
}
