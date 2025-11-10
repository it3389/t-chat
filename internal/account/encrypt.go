package account

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"

	"golang.org/x/crypto/pbkdf2"
)

// EncodePrivateKey 将RSA私钥编码为PEM格式字节切片
func EncodePrivateKey(priv *rsa.PrivateKey) []byte {
	privBytes := x509.MarshalPKCS1PrivateKey(priv)
	block := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	}
	return pem.EncodeToMemory(block)
}

// DecodePrivateKey 将PEM格式字节切片解码为RSA私钥
func DecodePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("无效的私钥PEM数据")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

// EncodePublicKey 将RSA公钥编码为PEM格式字节切片
func EncodePublicKey(pub *rsa.PublicKey) []byte {
	pubBytes := x509.MarshalPKCS1PublicKey(pub)
	block := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubBytes,
	}
	return pem.EncodeToMemory(block)
}

// DecodePublicKey 将PEM格式字节切片解码为RSA公钥
func DecodePublicKey(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PUBLIC KEY" {
		return nil, errors.New("无效的公钥PEM数据")
	}
	return x509.ParsePKCS1PublicKey(block.Bytes)
}

// EncryptPrivateKey 使用用户密码对私钥进行AES加密，返回密文
func EncryptPrivateKey(privPEM []byte, password string) ([]byte, error) {
	// 使用PBKDF2从密码派生密钥
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := pbkdf2.Key([]byte(password), salt, 4096, 32, sha256.New)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, privPEM, nil)

	// 输出格式: salt|nonce|ciphertext
	buf := bytes.Buffer{}
	buf.Write(salt)
	buf.Write(nonce)
	buf.Write(ciphertext)
	return buf.Bytes(), nil
}

// DecryptPrivateKey 使用用户密码解密私钥密文，返回原始PEM
func DecryptPrivateKey(enc []byte, password string) ([]byte, error) {
	if len(enc) < 16 {
		return nil, errors.New("密文数据过短")
	}
	salt := enc[:16]
	key := pbkdf2.Key([]byte(password), salt, 4096, 32, sha256.New)

	// 取nonce
	nonceSize := 12 // GCM标准nonce长度
	if len(enc) < 16+nonceSize {
		return nil, errors.New("密文数据过短")
	}
	nonce := enc[16 : 16+nonceSize]
	ciphertext := enc[16+nonceSize:]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plain, nil
}
