// Copyright © 2026 XunLink (讯联IM). All rights reserved.
//
// RSA 密钥管理：平台侧（admin-api）首次启动自动生成密钥对并落盘；
// 公钥文件随私有化安装包分发，用于验签。生产环境建议接入 KMS。

package license

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"

	"github.com/openimsdk/tools/errs"
)

const (
	PrivateKeyFile = "xlink-license-private.pem"
	PublicKeyFile  = "xlink-license-public.pem"
)

// LoadOrGenerateKeyPair 从 dir 加载密钥对；不存在则生成（2048 位）
func LoadOrGenerateKeyPair(dir string) (*rsa.PrivateKey, error) {
	privPath := filepath.Join(dir, PrivateKeyFile)
	pubPath := filepath.Join(dir, PublicKeyFile)

	if b, err := os.ReadFile(privPath); err == nil {
		block, _ := pem.Decode(b)
		if block == nil {
			return nil, errs.New("license private key pem malformed")
		}
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, errs.Wrap(err)
	}
	privPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(privPath, privPem, 0o600); err != nil { // 私钥仅属主可读
		return nil, errs.Wrap(err)
	}
	pubPem := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey)})
	if err := os.WriteFile(pubPath, pubPem, 0o644); err != nil {
		return nil, errs.Wrap(err)
	}
	return key, nil
}

// LoadPublicKey 从公钥文件加载（服务验签用）
func LoadPublicKey(path string) (*rsa.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errs.New("license public key pem malformed")
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}
