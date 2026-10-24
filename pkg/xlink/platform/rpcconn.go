// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层服务 → OpenIM rpc 连接构建（etcd 服务发现）

package platform

import (
	"context"

	kdisc "github.com/openimsdk/open-im-server/v3/pkg/common/discoveryregister"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/mw"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BuildRpcConn 建立指定 OpenIM rpc 的连接；name 取注册名（user/auth/friend...）
func BuildRpcConn(ctx context.Context, cfg *Config, name string) (grpc.ClientConnInterface, error) {
	dis := &config.Discovery{
		Enable: "etcd",
		Etcd: config.Etcd{
			RootDirectory: cfg.OpenIM.EtcdRootDir,
			Address:       cfg.OpenIM.EtcdAddress,
		},
	}
	share := &config.Share{}
	share.RpcRegisterName.User = "user"
	share.RpcRegisterName.Auth = "auth"
	client, err := kdisc.NewDiscoveryRegister(dis, share, nil)
	if err != nil {
		return nil, errs.WrapMsg(err, "discovery init failed")
	}
	client.AddOption(mw.GrpcClient(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(`{"LoadBalancingPolicy": "round_robin"}`))
	conn, err := client.GetConn(ctx, name)
	if err != nil {
		return nil, errs.WrapMsg(err, "connect rpc failed", "name", name)
	}
	return conn, nil
}
