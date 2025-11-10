package chat

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
)

// EncryptMessage 使用AES加密消息
// 返回Base64编码的密文、密钥和随机数
func EncryptMessage(plainText string) (cipherTextBase64 string, key []byte, nonce []byte, err error) {
	// 生成32字节的AES密钥
	key = make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, key); err != nil {
		return "", nil, nil, fmt.Errorf("生成密钥失败: %w", err)
	}

	// 创建AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", nil, nil, fmt.Errorf("创建AES cipher失败: %w", err)
	}

	// 使用GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", nil, nil, fmt.Errorf("创建GCM失败: %w", err)
	}

	// 生成随机数
	nonce = make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", nil, nil, fmt.Errorf("生成随机数失败: %w", err)
	}

	// 加密
	cipherText := gcm.Seal(nil, nonce, []byte(plainText), nil)
	cipherTextBase64 = base64.StdEncoding.EncodeToString(cipherText)

	return cipherTextBase64, key, nonce, nil
}

// DecryptMessage 使用AES解密消息
func DecryptMessage(cipherTextBase64 string, key []byte, nonce []byte) (plainText string, err error) {
	// 解码Base64
	cipherText, err := base64.StdEncoding.DecodeString(cipherTextBase64)
	if err != nil {
		return "", fmt.Errorf("解码Base64失败: %w", err)
	}

	// 创建AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %w", err)
	}

	// 使用GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建GCM失败: %w", err)
	}

	// 解密
	plainTextBytes, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	return string(plainTextBytes), nil
}

// EncryptKeyWithRSA 使用RSA加密AES密钥
func EncryptKeyWithRSA(aesKey []byte, pub *rsa.PublicKey) ([]byte, error) {
	return rsa.EncryptPKCS1v15(rand.Reader, pub, aesKey)
}

// DecryptKeyWithRSA 使用RSA解密AES密钥
func DecryptKeyWithRSA(cipherKey []byte, priv *rsa.PrivateKey) ([]byte, error) {
	return rsa.DecryptPKCS1v15(rand.Reader, priv, cipherKey)
}

// encryptWithAES 使用AES-GCM加密数据
func encryptWithAES(plainText string, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("创建AES cipher失败: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("创建GCM失败: %w", err)
	}

	cipherText := gcm.Seal(nil, nonce, []byte(plainText), nil)
	return cipherText, nil
}

// decryptWithAES 使用AES-GCM解密数据
func decryptWithAES(cipherText, key, nonce []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("创建AES cipher失败: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("创建GCM失败: %w", err)
	}

	plainTextBytes, err := gcm.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	return string(plainTextBytes), nil
}