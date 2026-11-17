// Package secretbox 提供数据库内敏感设置的对称加密（可选）。
// 密钥来自 ANIPICK_SECRET；未配置时设置以明文保存并附带标记。
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "enc:v1:"

type Box struct {
	aead cipher.AEAD
}

func New(secret string) (*Box, error) {
	if secret == "" {
		return nil, nil
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

func (b *Box) Seal(plain string) (string, error) {
	if b == nil || plain == "" {
		return plain, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.RawURLEncoding.EncodeToString(out), nil
}

func (b *Box) Open(cipherText string) (string, error) {
	if !strings.HasPrefix(cipherText, prefix) {
		return cipherText, nil
	}
	if b == nil {
		return "", errors.New("设置了 ANIPICK_SECRET 加密的数据，但当前进程未配置 ANIPICK_SECRET")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(cipherText, prefix))
	if err != nil {
		return "", err
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("密文长度非法")
	}
	out, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("解密失败（ANIPICK_SECRET 可能已变更）")
	}
	return string(out), nil
}
