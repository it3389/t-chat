// Package account 提供用户账户管理功能
//
// 账户管理模块负责处理用户账户的完整生命周期，包括：
// - 账户创建和删除
// - 密钥生成和管理
// - 账户认证和验证
// - 当前账户状态管理
// - 账户数据持久化
//
// 核心特性：
// - 基于Ed25519的公私钥加密
// - 密码保护的私钥存储
// - 并发安全的账户操作
// - 可扩展的存储和加密服务
package account

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Manager 账户管理器
//
// 核心功能：
// - 管理系统中的所有用户账户
// - 提供账户的CRUD操作
// - 维护当前活跃账户状态
// - 确保账户操作的并发安全
// - 协调存储和加密服务
//
// 设计目标：
// - 提供统一的账户管理接口
// - 确保账户数据的安全性
// - 支持多账户并存和切换
// - 提供可扩展的服务架构
type Manager struct {
	accounts       map[string]*Account // 内存中的账户缓存，键为用户名
	current        *Account            // 当前活跃账户实例
	mutex          sync.RWMutex        // 读写锁，保证并发安全访问
	storageService StorageService      // 存储服务接口，负责数据持久化
	cryptoService  CryptoService       // 加密服务接口，负责密钥管理
}

// NewManager 创建新的账户管理器
//
// 返回值：
//   *Manager - 新创建的账户管理器实例
//
// 处理逻辑：
//   1. 使用默认的存储服务
//   2. 使用默认的加密服务
//   3. 调用NewManagerWithServices创建实例
//
// 注意：
//   此函数是NewManagerWithServices的便捷包装
func NewManager() *Manager {
	return NewManagerWithServices(GetStorageService(), GetCryptoService())
}

// NewManagerWithServices 使用指定服务创建账户管理器
//
// 参数：
//   storageService - 存储服务实例，负责账户数据持久化
//   cryptoService - 加密服务实例，负责密钥生成和管理
//
// 返回值：
//   *Manager - 新创建的账户管理器实例
//
// 处理逻辑：
//   1. 初始化账户缓存映射
//   2. 设置存储和加密服务
//   3. 加载现有账户数据
//   4. 处理加载错误但不阻止创建
//
// 设计优势：
//   - 支持依赖注入，便于测试
//   - 允许自定义存储和加密实现
//   - 提供灵活的服务扩展能力
func NewManagerWithServices(storageService StorageService, cryptoService CryptoService) *Manager {
	m := &Manager{
		accounts:       make(map[string]*Account),
		storageService: storageService,
		cryptoService:  cryptoService,
	}
	
	// 加载现有账户
	if err := m.loadAccounts(); err != nil {
		// 加载失败时记录错误，但不阻止管理器创建
		fmt.Printf("警告：加载账户失败: %v\n", err)
	}
	
	return m
}

// CreateAccount 创建账户
// 在系统中创建一个新的账户，包括生成密钥对和持久化存储
// 参数：
//   - username: 用户名，必须唯一
//   - password: 明文密码
// 返回：
//   - *Account: 创建的账户实例
//   - error: 创建过程中的错误
func (m *Manager) CreateAccount(username, password string) (*Account, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查账户是否已存在
	if _, exists := m.accounts[username]; exists {
		return nil, fmt.Errorf("账户已存在: %s", username)
	}

	// 创建新账户
	account, err := NewAccount(username, password)
	if err != nil {
		return nil, fmt.Errorf("创建账户失败: %v", err)
	}

	// 保存到存储
	if err := m.storageService.SaveAccount(account); err != nil {
		return nil, fmt.Errorf("保存账户失败: %v", err)
	}

	// 添加到内存缓存
	m.accounts[username] = account
	
	// 设置为当前账户
	m.current = account

	return account, nil
}

// CreateAccountWithPassword 使用用户名和密码创建账户
// 这是CreateAccount的别名方法，保持向后兼容性
// 参数：
//   - username: 用户名
//   - password: 密码
// 返回：
//   - *Account: 创建的账户实例
//   - error: 创建过程中的错误
func (m *Manager) CreateAccountWithPassword(username, password string) (*Account, error) {
	return m.CreateAccount(username, password)
}

// GetAccount 获取账户
func (m *Manager) GetAccount(username string) *Account {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.accounts[username]
}

// GetCurrentAccount 获取当前账户
func (m *Manager) GetCurrentAccount() *Account {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.current
}

// SetCurrentAccount 设置当前账户
func (m *Manager) SetCurrentAccount(username string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	acc, exists := m.accounts[username]
	if !exists {
		return fmt.Errorf("账户不存在 %s", username)
	}

	m.current = acc
	return nil
}

// SetCurrentAccountWithPassword 设置当前账户并解密公钥
func (m *Manager) SetCurrentAccountWithPassword(username, password string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	acc, exists := m.accounts[username]
	if !exists {
		return fmt.Errorf("账户不存在 %s", username)
	}

	// 验证密码
	if !m.CheckPassword(acc, password) {
		return fmt.Errorf("密码错误")
	}

	// 解密公钥并设置到明文字段
	pub, _, err := m.DecryptAccountKeys(acc, password)
	if err != nil {
		return fmt.Errorf("解密公钥失败: %v", err)
	}

	// 设置明文公钥
	acc.PublicKey = fmt.Sprintf("%x", pub)
	m.current = acc
	return nil
}

// GetAllAccounts 获取所有账户
func (m *Manager) GetAllAccounts() []*Account {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	accounts := make([]*Account, 0, len(m.accounts))
	for _, acc := range m.accounts {
		accounts = append(accounts, acc)
	}

	// 按用户名排序，确保顺序稳定
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Username < accounts[j].Username
	})

	return accounts
}

// DeleteAccount 删除账户
func (m *Manager) DeleteAccount(username string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.accounts[username]; !exists {
		return fmt.Errorf("账户不存在 %s", username)
	}

	delete(m.accounts, username)
	dir := filepath.Join("accounts", username)
	os.RemoveAll(dir)

	// 如果删除的是当前账户，清空当前账户
	if m.current != nil && m.current.Username == username {
		m.current = nil
	}

	return nil
}

// saveAccountFile 保存单个账户到 accounts/<username>/account.json
func (m *Manager) saveAccountFile(acc *Account) error {
	dir := filepath.Join("accounts", acc.Username)
	os.MkdirAll(dir, 0755)
	file := filepath.Join(dir, "account.json")
	data, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0644)
}

// saveAccounts 仅用于兼容旧接口，保存所有账户（可选实现，不再用）
func (m *Manager) saveAccounts() error {

	for _, acc := range m.accounts {
		if err := m.saveAccountFile(acc); err != nil {
			return err
		}
	}
	return nil
}

// loadAccounts 加载所有账户
func (m *Manager) loadAccounts() error {
	m.accounts = make(map[string]*Account)
	dirs, err := os.ReadDir("accounts")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		file := filepath.Join("accounts", dir.Name(), "account.json")
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		acc := &Account{}
		if err := json.Unmarshal(data, acc); err != nil {
			continue
		}
		m.accounts[acc.Username] = acc
	}
	return nil
}

// 注意：hashPassword, EncryptBytes, decryptBytes 函数已移至 crypto.go 文件

// DecryptAccountKeys 解密账户密钥，返回明文公私钥
func (m *Manager) DecryptAccountKeys(acc *Account, password string) (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pubBytes, err := m.cryptoService.DecryptBytes(acc.PublicKeyEnc, password)
	if err != nil {
		return nil, nil, fmt.Errorf("公钥解密失败: %v", err)
	}
	privBytes, err := m.cryptoService.DecryptBytes(acc.PrivateKeyEnc, password)
	if err != nil {
		return nil, nil, fmt.Errorf("私钥解密失败: %v", err)
	}
	return ed25519.PublicKey(pubBytes), ed25519.PrivateKey(privBytes), nil
}

// CheckPassword 登录时密码校验
func (m *Manager) CheckPassword(acc *Account, password string) bool {
	return acc.PasswordHash == m.cryptoService.HashPassword(password)
}

var globalMgr *Manager

func init() {
	globalMgr = nil
}

// SetGlobalManager 设置全局账户管理器
func SetGlobalManager(m *Manager) {
	globalMgr = m
}

// GetCurrentAccount 全局获取当前账户
func GetCurrentAccount() *Account {
	if globalMgr == nil {
		return nil
	}
	return globalMgr.GetCurrentAccount()
}
