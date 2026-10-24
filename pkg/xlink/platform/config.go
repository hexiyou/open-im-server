// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// 平台层服务（saas-api / admin-api）配置加载：单一 config/xlink.yml

package platform

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	AdminAPI struct {
		ListenIP string `mapstructure:"listenIP"`
		Ports    []int  `mapstructure:"ports"`
	} `mapstructure:"adminAPI"`
	SaasAPI struct {
		ListenIP string `mapstructure:"listenIP"`
		Ports    []int  `mapstructure:"ports"`
	} `mapstructure:"saasAPI"`
	Mongo struct {
		Address     []string `mapstructure:"address"`
		Database    string   `mapstructure:"database"`
		Username    string   `mapstructure:"username"`
		Password    string   `mapstructure:"password"`
		AuthSource  string   `mapstructure:"authSource"`
		MaxPoolSize int      `mapstructure:"maxPoolSize"`
	} `mapstructure:"mongo"`
	JWT struct {
		PlatformSecret string `mapstructure:"platformSecret"`
		TenantSecret   string `mapstructure:"tenantSecret"`
		ExpireDays     int    `mapstructure:"expireDays"`
	} `mapstructure:"jwt"`
	OpenIM struct {
		TokenSecret   string   `mapstructure:"tokenSecret"`
		IMAdminUserID string   `mapstructure:"imAdminUserID"`
		EtcdAddress   []string `mapstructure:"etcdAddress"`
		EtcdRootDir   string   `mapstructure:"etcdRootDir"`
	} `mapstructure:"openim"`
	Redis struct {
		Address  string `mapstructure:"address"`
		Password string `mapstructure:"password"`
	} `mapstructure:"redis"`
	Guest struct {
		RateLimitPerHour int64 `mapstructure:"rateLimitPerHour"` // 单IP单租户每小时 init 次数上限（42901）
	} `mapstructure:"guest"`
	License struct {
		Enabled          bool   `mapstructure:"enabled"`          // 私有化部署 true；SaaS 平台侧 false
		KeyDir           string `mapstructure:"keyDir"`           // RSA 密钥目录（admin-api 私钥）
		PublicKeyPath    string `mapstructure:"publicKeyPath"`    // 公钥路径（验签）
		LocalDir         string `mapstructure:"localDir"`         // 本地授权文件目录
		PlatformURL      string `mapstructure:"platformURL"`      // 心跳目标（平台校验服务）
		HeartbeatMinutes int    `mapstructure:"heartbeatMinutes"` // 心跳间隔（分钟）
	} `mapstructure:"license"`
	Seed struct {
		SuperUser     string `mapstructure:"superUser"`
		SuperPassword string `mapstructure:"superPassword"`
	} `mapstructure:"seed"`
}

func LoadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// JoinAddrs MongoDB 多地址逗号拼接（URI host 部分）
func JoinAddrs(addrs []string) string {
	return strings.Join(addrs, ",")
}
