package account

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// StorageService 存储服务接口
// 定义了账户数据持久化的操作
type StorageService interface {
	// SaveAccount 保存账户数据
	SaveAccount(account *Account) error
	
	// LoadAccount 加载账户数据
	LoadAccount(username string) (*Account, error)
	
	// DeleteAccount 删除账户数据
	DeleteAccount(username string) error
	
	// ListAccounts 列出所有账户用户名
	ListAccounts() ([]string, error)
	
	// AccountExists 检查账户是否存在
	AccountExists(username string) bool
}

// FileStorageService 基于文件系统的存储服务
// 将账户数据存储在本地文件系统中
type FileStorageService struct {
	basePath string // 存储根目录
}

// NewFileStorageService 创建文件存储服务实例
// 参数：
//   - basePath: 存储根目录路径
func NewFileStorageService(basePath string) *FileStorageService {
	service := &FileStorageService{
		basePath: basePath,
	}
	
	// 确保存储目录存在
	if err := service.ensureBaseDir(); err != nil {
		// 如果创建目录失败，使用默认路径
		service.basePath = "accounts"
		service.ensureBaseDir()
	}
	
	return service
}

// ensureBaseDir 确保基础目录存在
func (fs *FileStorageService) ensureBaseDir() error {
	return os.MkdirAll(fs.basePath, 0755)
}

// getAccountDir 获取账户的存储目录
func (fs *FileStorageService) getAccountDir(username string) string {
	return filepath.Join(fs.basePath, username)
}

// getAccountFile 获取账户的存储文件路径
func (fs *FileStorageService) getAccountFile(username string) string {
	return filepath.Join(fs.getAccountDir(username), "account.json")
}

// SaveAccount 保存账户数据到文件
// 将账户数据序列化为JSON格式并保存到对应的文件中
// 参数：
//   - account: 要保存的账户实例
// 返回：
//   - error: 保存过程中的错误
func (fs *FileStorageService) SaveAccount(account *Account) error {
	if account == nil {
		return fmt.Errorf("账户不能为空")
	}
	
	if !account.IsValid() {
		return fmt.Errorf("账户数据无效")
	}

	// 创建账户目录
	accountDir := fs.getAccountDir(account.Username)
	if err := os.MkdirAll(accountDir, 0755); err != nil {
		return fmt.Errorf("创建账户目录失败: %v", err)
	}

	// 序列化账户数据
	data, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化账户数据失败: %v", err)
	}

	// 写入文件
	accountFile := fs.getAccountFile(account.Username)
	if err := os.WriteFile(accountFile, data, 0644); err != nil {
		return fmt.Errorf("写入账户文件失败: %v", err)
	}

	return nil
}

// LoadAccount 从文件加载账户数据
// 从对应的文件中读取并反序列化账户数据
// 参数：
//   - username: 要加载的账户用户名
// 返回：
//   - *Account: 加载的账户实例
//   - error: 加载过程中的错误
func (fs *FileStorageService) LoadAccount(username string) (*Account, error) {
	if username == "" {
		return nil, fmt.Errorf("用户名不能为空")
	}

	accountFile := fs.getAccountFile(username)
	
	// 检查文件是否存在
	if _, err := os.Stat(accountFile); os.IsNotExist(err) {
		return nil, fmt.Errorf("账户不存在: %s", username)
	}

	// 读取文件内容
	data, err := os.ReadFile(accountFile)
	if err != nil {
		return nil, fmt.Errorf("读取账户文件失败: %v", err)
	}

	// 反序列化账户数据
	account := &Account{}
	if err := json.Unmarshal(data, account); err != nil {
		return nil, fmt.Errorf("反序列化账户数据失败: %v", err)
	}

	// 验证账户数据
	if !account.IsValid() {
		return nil, fmt.Errorf("账户数据无效")
	}

	return account, nil
}

// DeleteAccount 删除账户数据
// 删除账户对应的整个目录及其内容
// 参数：
//   - username: 要删除的账户用户名
// 返回：
//   - error: 删除过程中的错误
func (fs *FileStorageService) DeleteAccount(username string) error {
	if username == "" {
		return fmt.Errorf("用户名不能为空")
	}

	accountDir := fs.getAccountDir(username)
	
	// 检查目录是否存在
	if _, err := os.Stat(accountDir); os.IsNotExist(err) {
		return fmt.Errorf("账户不存在: %s", username)
	}

	// 删除整个账户目录
	if err := os.RemoveAll(accountDir); err != nil {
		return fmt.Errorf("删除账户目录失败: %v", err)
	}

	return nil
}

// ListAccounts 列出所有账户用户名
// 扫描存储目录，返回所有有效的账户用户名
// 返回：
//   - []string: 账户用户名列表
//   - error: 扫描过程中的错误
func (fs *FileStorageService) ListAccounts() ([]string, error) {
	// 读取基础目录
	entries, err := os.ReadDir(fs.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取账户目录失败: %v", err)
	}

	var accounts []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		username := entry.Name()
		// 验证账户文件是否存在且有效
		if fs.AccountExists(username) {
			accounts = append(accounts, username)
		}
	}

	// 对账户列表进行排序，确保顺序稳定
	sort.Strings(accounts)
	return accounts, nil
}

// AccountExists 检查账户是否存在
// 检查指定用户名的账户文件是否存在且有效
// 参数：
//   - username: 要检查的账户用户名
// 返回：
//   - bool: 账户是否存在
func (fs *FileStorageService) AccountExists(username string) bool {
	if username == "" {
		return false
	}

	accountFile := fs.getAccountFile(username)
	
	// 检查文件是否存在
	if _, err := os.Stat(accountFile); os.IsNotExist(err) {
		return false
	}

	// 尝试加载账户以验证数据有效性
	_, err := fs.LoadAccount(username)
	return err == nil
}

// 全局存储服务实例
var defaultStorageService StorageService = NewFileStorageService("accounts")

// SetStorageService 设置全局存储服务
// 允许替换默认的存储服务实现
func SetStorageService(service StorageService) {
	defaultStorageService = service
}

// GetStorageService 获取当前的存储服务
func GetStorageService() StorageService {
	return defaultStorageService
}