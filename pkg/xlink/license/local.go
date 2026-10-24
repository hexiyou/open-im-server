// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 本地授权文件：私有化实例激活后落盘（激活码原文 + 运行时状态），
// 服务启动与门禁读取；在线心跳结果回写（LastOKHB / 状态 / 吊销标记）。

package license

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/openimsdk/tools/errs"
)

func timeNow() int64 { return time.Now().Unix() }

// LocalLicense 本地授权文件（与激活码同目录存储，默认 config/）
type LocalLicense struct {
	ActivationCode string     `json:"activationCode"` // 签名激活码原文
	Payload        *LicensePayload `json:"payload"`    // 验签后的载荷（冗余存储，避免每次验签）
	State          LocalState `json:"state"`
}

// LoadLocal 从 dir/xlink-license-local.json 加载；不存在返回 nil（未激活）
func LoadLocal(dir string) (*LocalLicense, error) {
	b, err := os.ReadFile(filepath.Join(dir, "xlink-license-local.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errs.Wrap(err)
	}
	var ll LocalLicense
	if err := json.Unmarshal(b, &ll); err != nil {
		return nil, errs.Wrap(err)
	}
	return &ll, nil
}

// SaveLocal 写入本地授权文件
func SaveLocal(dir string, ll *LocalLicense) error {
	b, err := json.MarshalIndent(ll, "", "  ")
	if err != nil {
		return errs.Wrap(err)
	}
	return errs.Wrap(os.WriteFile(filepath.Join(dir, "xlink-license-local.json"), b, 0o644))
}

// Activate 验签激活码并落盘（指纹必须与本机一致，一机一码）
func Activate(dir, activationCode, publicKeyPath string) (*LocalLicense, error) {
	pub, err := LoadPublicKey(publicKeyPath)
	if err != nil {
		return nil, errs.WrapMsg(err, "load public key failed")
	}
	payload, err := VerifyLicense(activationCode, pub)
	if err != nil {
		return nil, err
	}
	fp, err := MachineFingerprint()
	if err != nil {
		return nil, err
	}
	if fp != payload.MachineFP {
		return nil, errs.ErrArgs.WrapMsg("machine fingerprint mismatch, license bound to another machine")
	}
	ll := &LocalLicense{
		ActivationCode: activationCode,
		Payload:        payload,
		State:          LocalState{Status: StatusActivated},
	}
	if err := SaveLocal(dir, ll); err != nil {
		return nil, err
	}
	return ll, nil
}

// CurrentState 计算当前运行时状态（含错误码语义：activated→nil；grace→45102；其余→45101）
func CurrentState(dir string) (*LocalLicense, string, error) {
	ll, err := LoadLocal(dir)
	if err != nil {
		return nil, StatusDegraded, ErrLicenseDegraded.WrapMsg("local license unreadable")
	}
	if ll == nil || ll.Payload == nil {
		return nil, StatusUnactivated, ErrLicenseDegraded.WrapMsg("not activated")
	}
	status, _, err := Evaluate(ll.Payload, &ll.State, timeNow())
	return ll, status, err
}
