// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// license-cli 私有化部署授权工具（安装向导后端）：
//   openim-license-cli fingerprint                        打印本机指纹
//   openim-license-cli apply -merchant 商户名             生成申请码
//   openim-license-cli activate -code <激活码> -pubkey <公钥路径> -dir <配置目录>
//   openim-license-cli status -dir <配置目录>             查看本地授权状态

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/xlink/license"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	var err error
	switch os.Args[1] {
	case "fingerprint":
		var fp string
		fp, err = license.MachineFingerprint()
		if err == nil {
			fmt.Println(fp)
		}
	case "apply":
		fs := flag.NewFlagSet("apply", flag.ExitOnError)
		merchant := fs.String("merchant", "", "商户名称")
		_ = fs.Parse(os.Args[2:])
		if *merchant == "" {
			fmt.Println("缺少 -merchant 参数")
			os.Exit(1)
		}
		var fp string
		fp, err = license.MachineFingerprint()
		if err != nil {
			break
		}
		code := license.EncodeApplyCode(&license.ApplyCode{
			Merchant:    *merchant,
			MachineFP:   fp,
			RequestedAt: time.Now().Unix(),
		})
		fmt.Println("machineFP: " + fp)
		fmt.Println("applyCode: " + code)
	case "activate":
		fs := flag.NewFlagSet("activate", flag.ExitOnError)
		code := fs.String("code", "", "平台签发的激活码")
		pubkey := fs.String("pubkey", "./config/xlink-license-public.pem", "平台公钥路径")
		dir := fs.String("dir", "./config", "本地授权文件目录")
		_ = fs.Parse(os.Args[2:])
		if *code == "" {
			fmt.Println("缺少 -code 参数")
			os.Exit(1)
		}
		var ll *license.LocalLicense
		ll, err = license.Activate(*dir, *code, *pubkey)
		if err == nil {
			fmt.Printf("激活成功：licenseNo=%s merchant=%s 有效期至 %d (mode=%s seats=%d)\n",
				ll.Payload.LicenseID, ll.Payload.Merchant, ll.Payload.NotAfter, ll.Payload.Mode, ll.Payload.Seats)
		}
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		dir := fs.String("dir", "./config", "本地授权文件目录")
		_ = fs.Parse(os.Args[2:])
		var ll *license.LocalLicense
		var status string
		ll, status, err = license.CurrentState(*dir)
		if ll == nil {
			fmt.Println("状态:", status, "（未激活）")
			return
		}
		fmt.Printf("状态: %s\n授权号: %s\n商户: %s\n模式: %s\n到期时间戳: %d\n最近成功心跳: %d\n",
			status, ll.Payload.LicenseID, ll.Payload.Merchant, ll.Payload.Mode, ll.Payload.NotAfter, ll.State.LastOKHB)
		if err != nil {
			fmt.Println("提示:", err.Error())
		}
	default:
		usage()
		os.Exit(1)
	}
	if err != nil {
		fmt.Println("错误:", err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`用法:
  openim-license-cli fingerprint
  openim-license-cli apply -merchant <商户名>
  openim-license-cli activate -code <激活码> [-pubkey <公钥路径>] [-dir <配置目录>]
  openim-license-cli status [-dir <配置目录>]`)
}
