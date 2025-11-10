package account

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// CryptoService 加密服务接口
// 定义了账户系统所需的加密解密操作
type CryptoService interface {
	// EncryptBytes 加密字节数据
	EncryptBytes(data []byte, password string) ([]byte, error)
	
	// DecryptBytes 解密字节数据
	DecryptBytes(encryptedHex, password string) ([]byte, error)
	
	// HashPassword 计算密码哈希
	HashPassword(password string) string
}

// SimpleCryptoService 简单加密服务实现
// 使用XOR加密算法的简单实现，适用于演示和测试
type SimpleCryptoService struct{}

// NewSimpleCryptoService 创建简单加密服务实例
func NewSimpleCryptoService() *SimpleCryptoService {
	return &SimpleCryptoService{}
}

// EncryptBytes 使用密码加密字节数据
// 使用SHA256生成密钥，然后进行XOR加密
// 参数：
//   - data: 待加密的字节数据
//   - password: 加密密码
// 返回：
//   - []byte: 加密后的字节数据
//   - error: 加密过程中的错误
func (s *SimpleCryptoService) EncryptBytes(data []byte, password string) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("待加密数据不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("加密密码不能为空")
	}

	// 使用密码生成密钥
	key := sha256.Sum256([]byte(password))
	
	// XOR加密
	encrypted := make([]byte, len(data))
	for i := range data {
		encrypted[i] = data[i] ^ key[i%len(key)]
	}
	
	return encrypted, nil
}

// DecryptBytes 使用密码解密字节数据
// 先将十六进制字符串解码，然后进行XOR解密
// 参数：
//   - encryptedHex: 加密数据的十六进制字符串
//   - password: 解密密码
// 返回：
//   - []byte: 解密后的字节数据
//   - error: 解密过程中的错误
func (s *SimpleCryptoService) DecryptBytes(encryptedHex, password string) ([]byte, error) {
	if encryptedHex == "" {
		return nil, fmt.Errorf("加密数据不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("解密密码不能为空")
	}

	// 解码十六进制字符串
	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return nil, fmt.Errorf("十六进制解码失败: %v", err)
	}

	// 使用密码生成密钥
	key := sha256.Sum256([]byte(password))
	
	// XOR解密
	decrypted := make([]byte, len(encrypted))
	for i := range encrypted {
		decrypted[i] = encrypted[i] ^ key[i%len(key)]
	}
	
	return decrypted, nil
}

// HashPassword 计算密码的SHA256哈希值
// 参数：
//   - password: 明文密码
// 返回：
//   - string: 密码的十六进制哈希值
func (s *SimpleCryptoService) HashPassword(password string) string {
	h := sha256.Sum256([]byte(password))
	return hex.EncodeToString(h[:])
}

// 全局加密服务实例
var defaultCryptoService CryptoService = NewSimpleCryptoService()

// SetCryptoService 设置全局加密服务
// 允许替换默认的加密服务实现
func SetCryptoService(service CryptoService) {
	defaultCryptoService = service
}

// GetCryptoService 获取当前的加密服务
func GetCryptoService() CryptoService {
	return defaultCryptoService
}

// 注意：便利函数 encryptBytes 和 decryptBytes 已移除
// 请直接使用 GetCryptoService().EncryptBytes() 和 GetCryptoService().DecryptBytes()