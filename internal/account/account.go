package account

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"time"
)

// Account 账户结构体
// 表示系统中的一个用户账户，包含用户名、密码哈希、加密的密钥对等信息
type Account struct {
	Username      string    `json:"username"`       // 用户名，唯一标识符
	PasswordHash  string    `json:"password_hash"`  // 密码哈希（SHA256）
	PublicKey     string    `json:"-"`              // 明文公钥（hex编码，不持久化）
	PublicKeyEnc  string    `json:"public_key_enc"` // 用密码加密的公钥（hex编码）
	PrivateKeyEnc string    `json:"private_key_enc"`// 用密码加密的私钥（hex编码）
	CreatedAt     time.Time `json:"created_at"`     // 账户创建时间
}

// NewAccount 创建新的账户实例
// 参数：
//   - username: 用户名
//   - password: 明文密码
// 返回：
//   - *Account: 新创建的账户实例
//   - error: 创建过程中的错误
func NewAccount(username, password string) (*Account, error) {
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}
	if password == "" {
		return nil, fmt.Errorf("密码不能为空")
	}
	if len(password) < 6 {
		return nil, fmt.Errorf("密码长度不能少于6位")
	}

	account := &Account{
		Username:     username,
		PasswordHash: GetCryptoService().HashPassword(password),
		CreatedAt:    time.Now(),
	}

	// 生成并加密密钥对
	if err := account.generateAndEncryptKeys(password); err != nil {
		return nil, fmt.Errorf("密钥生成失败: %v", err)
	}

	return account, nil
}

// generateAndEncryptKeys 生成并加密密钥对
// 生成Ed25519密钥对，并使用密码进行加密存储
func (a *Account) generateAndEncryptKeys(password string) error {
	// 生成Ed25519密钥对
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return fmt.Errorf("密钥对生成失败: %v", err)
	}

	// 加密公钥
	pubEnc, err := GetCryptoService().EncryptBytes(pub, password)
	if err != nil {
		return fmt.Errorf("公钥加密失败: %v", err)
	}

	// 加密私钥
	privEnc, err := GetCryptoService().EncryptBytes(priv, password)
	if err != nil {
		return fmt.Errorf("私钥加密失败: %v", err)
	}

	// 存储加密后的密钥
	a.PublicKeyEnc = hex.EncodeToString(pubEnc)
	a.PrivateKeyEnc = hex.EncodeToString(privEnc)

	return nil
}

// CheckPassword 验证密码是否正确
// 参数：
//   - password: 待验证的明文密码
// 返回：
//   - bool: 密码是否正确
func (a *Account) CheckPassword(password string) bool {
	return a.PasswordHash == GetCryptoService().HashPassword(password)
}

// DecryptKeys 解密账户密钥
// 使用提供的密码解密存储的密钥对
// 参数：
//   - password: 用于解密的密码
// 返回：
//   - ed25519.PublicKey: 解密后的公钥
//   - ed25519.PrivateKey: 解密后的私钥
//   - error: 解密过程中的错误
func (a *Account) DecryptKeys(password string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	// 验证密码
	if !a.CheckPassword(password) {
		return nil, nil, fmt.Errorf("密码错误")
	}

	// 解密公钥
	pubBytes, err := GetCryptoService().DecryptBytes(a.PublicKeyEnc, password)
	if err != nil {
		return nil, nil, fmt.Errorf("公钥解密失败: %v", err)
	}

	// 解密私钥
	privBytes, err := GetCryptoService().DecryptBytes(a.PrivateKeyEnc, password)
	if err != nil {
		return nil, nil, fmt.Errorf("私钥解密失败: %v", err)
	}

	return ed25519.PublicKey(pubBytes), ed25519.PrivateKey(privBytes), nil
}

// GetPublicKeyHex 获取公钥的十六进制表示
// 参数：
//   - password: 用于解密的密码
// 返回：
//   - string: 公钥的十六进制字符串
//   - error: 解密过程中的错误
func (a *Account) GetPublicKeyHex(password string) (string, error) {
	pub, _, err := a.DecryptKeys(password)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(pub), nil
}

// IsValid 验证账户数据的完整性
// 检查账户的必要字段是否都已设置
// 返回：
//   - bool: 账户数据是否有效
func (a *Account) IsValid() bool {
	return a.Username != "" &&
		a.PasswordHash != "" &&
		a.PublicKeyEnc != "" &&
		a.PrivateKeyEnc != "" &&
		!a.CreatedAt.IsZero()
}

// String 返回账户的字符串表示
// 用于调试和日志输出，不包含敏感信息
func (a *Account) String() string {
	return fmt.Sprintf("Account{Username: %s, CreatedAt: %s}", 
		a.Username, a.CreatedAt.Format("2006-01-02 15:04:05"))
}

// 注意：hashPassword 函数已移至 crypto.go 文件，通过 CryptoService 接口使用