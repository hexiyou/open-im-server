// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// saas-api 商户后台服务入口（API-TEN-xx/C-xx/G-xx，监听 10300）

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/openimsdk/open-im-server/v3/internal/xlink/saasapi"
	"github.com/openimsdk/open-im-server/v3/pkg/xlink/platform"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	configPath := flag.String("c", "./config/xlink.yml", "path of xlink config file")
	flag.Parse()

	cfg, err := platform.LoadConfig(*configPath)
	if err != nil {
		panic(fmt.Sprintf("load config failed: %v", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := connectMongo(ctx, cfg)
	if err != nil {
		panic(fmt.Sprintf("connect mongo failed: %v", err))
	}
	store, err := platform.NewStore(ctx, db)
	if err != nil {
		panic(fmt.Sprintf("init platform store failed: %v", err))
	}

	userConn, err := platform.BuildRpcConn(context.Background(), cfg, "user")
	if err != nil {
		panic(fmt.Sprintf("connect openim user rpc failed: %v", err))
	}
	authConn, err := platform.BuildRpcConn(context.Background(), cfg, "auth")
	if err != nil {
		panic(fmt.Sprintf("connect openim auth rpc failed: %v", err))
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Address, Password: cfg.Redis.Password})

	srv := saasapi.NewServer(cfg, store, userConn, authConn, rdb)
	srv.StartBackgroundTasks()
	port := cfg.SaasAPI.Ports[0]
	addr := fmt.Sprintf("%s:%d", cfg.SaasAPI.ListenIP, port)
	fmt.Printf("[xlink-saas-api] listening on %s\n", addr)
	server := &http.Server{Addr: addr, Handler: srv.Router()}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

func connectMongo(ctx context.Context, cfg *platform.Config) (*mongo.Database, error) {
	m := cfg.Mongo
	var uri string
	if m.Username != "" {
		uri = fmt.Sprintf("mongodb://%s:%s@%s/%s?authSource=%s&maxPoolSize=%d",
			m.Username, m.Password, platform.JoinAddrs(m.Address), m.AuthSource, m.AuthSource, m.MaxPoolSize)
	} else {
		uri = fmt.Sprintf("mongodb://%s/?maxPoolSize=%d", platform.JoinAddrs(m.Address), m.MaxPoolSize)
	}
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return client.Database(m.Database), nil
}
