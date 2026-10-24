// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 机器指纹（一机一码绑定依据，docs/02 §4.3）
// 因子：Linux 取 /etc/machine-id（回退 /var/lib/dbus/machine-id、DMI product_uuid）；
//      Windows 取注册表 MachineGuid。因子串加盐后 sha256，全长 64 hex。

package license

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/openimsdk/tools/errs"
)

const fingerprintSalt = "XunLink-License-FP-v1"

// MachineFingerprint 采集当前机器指纹（64 hex 字符）
func MachineFingerprint() (string, error) {
	var machineID string
	var err error
	switch runtime.GOOS {
	case "linux":
		machineID, err = linuxMachineID()
	case "windows":
		machineID, err = windowsMachineGUID()
	default:
		return "", errs.New(fmt.Sprintf("fingerprint unsupported on %s", runtime.GOOS))
	}
	if err != nil {
		return "", errs.WrapMsg(err, "collect machine id failed")
	}
	digest := sha256.Sum256([]byte(fingerprintSalt + machineID))
	return hex.EncodeToString(digest[:]), nil
}

func linuxMachineID() (string, error) {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id", "/sys/class/dmi/id/product_uuid"} {
		b, err := os.ReadFile(p)
		if err == nil {
			if v := strings.TrimSpace(string(b)); v != "" {
				return v, nil
			}
		}
	}
	return "", errs.New("no machine-id source found on linux")
}

func windowsMachineGUID() (string, error) {
	out, err := exec.Command("reg", "query",
		`HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
	if err != nil {
		return "", errs.Wrap(err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "MachineGuid") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[len(parts)-1], nil
			}
		}
	}
	return "", errs.New("MachineGuid not found in registry output")
}
