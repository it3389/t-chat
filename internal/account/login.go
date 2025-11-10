// Package account 提供用户账户管理功能
package account

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// LoginService 登录服务接口
//
// 定义登录相关的核心功能：
// - 用户身份验证
// - 账户选择和创建
// - 登录流程管理
type LoginService interface {
	// RequireLogin 强制要求用户登录
	RequireLogin() error
	
	// RequireLoginWithPassword 强制要求用户登录并返回密码
	RequireLoginWithPassword() (string, error)
	
	// AutoLogin 使用用户名和密码自动登录
	AutoLogin(username, password string) error
	
	// AutoLoginWithPassword 使用用户名和密码自动登录并返回密码
	AutoLoginWithPassword(username, password string) (string, error)
	
	// LoginExistingAccount 登录现有账户
	LoginExistingAccount(accounts []*Account) error
	
	// LoginExistingAccountWithPassword 登录现有账户并返回密码
	LoginExistingAccountWithPassword(accounts []*Account) (string, error)
	
	// CreateNewAccount 创建新账户
	CreateNewAccount() error
	
	// ReadPassword 安全地读取密码
	ReadPassword() (string, error)
}

// DefaultLoginService 默认登录服务实现
//
// 核心功能：
// - 提供完整的登录流程管理
// - 支持现有账户登录和新账户创建
// - 确保用户输入的安全性
// - 维护登录状态管理
//
// 设计目标：
// - 遵循面向对象设计原则
// - 提供清晰的职责分离
// - 支持可扩展的登录策略
type DefaultLoginService struct {
	accountManager *Manager // 账户管理器实例
}

// NewLoginService 创建新的登录服务实例
//
// 参数：
//   accountManager - 账户管理器实例
//
// 返回值：
//   LoginService - 登录服务接口实例
func NewLoginService(accountManager *Manager) LoginService {
	return &DefaultLoginService{
		accountManager: accountManager,
	}
}

// RequireLogin 强制要求用户登录
//
// 处理逻辑：
//   1. 获取所有可用账户
//   2. 如果没有账户，引导创建新账户
//   3. 显示账户列表供用户选择
//   4. 验证用户输入的密码
//   5. 设置当前登录账户
//
// 返回值：
//   error - 登录过程中的错误
func (ls *DefaultLoginService) RequireLogin() error {
	_, err := ls.RequireLoginWithPassword()
	return err
}

// AutoLogin 使用用户名和密码自动登录
func (ls *DefaultLoginService) AutoLogin(username, password string) error {
	_, err := ls.AutoLoginWithPassword(username, password)
	return err
}

// AutoLoginWithPassword 使用用户名和密码自动登录并返回密码
func (ls *DefaultLoginService) AutoLoginWithPassword(username, password string) (string, error) {
	fmt.Printf("\n=== TChat 自动登录 (用户: %s) ===\n", username)
	
	// 获取所有可用账户
	accounts := ls.accountManager.GetAllAccounts()
	
	// 查找指定用户名的账户
	var targetAccount *Account
	for _, account := range accounts {
		if account.Username == username {
			targetAccount = account
			break
		}
	}
	
	if targetAccount == nil {
		return "", fmt.Errorf("未找到用户名为 '%s' 的账户", username)
	}
	
	// 验证密码并登录
	err := ls.accountManager.SetCurrentAccountWithPassword(targetAccount.Username, password)
	if err != nil {
		return "", fmt.Errorf("自动登录失败: %v", err)
	}
	

	return password, nil
}

// RequireLoginWithPassword 强制要求用户登录并返回密码
//
// 处理逻辑：
//   1. 获取所有可用账户
//   2. 如果没有账户，引导创建新账户
//   3. 显示账户列表供用户选择
//   4. 验证用户输入的密码
//   5. 设置当前登录账户
//
// 返回值：
//   string - 用户输入的密码
//   error - 登录过程中的错误
func (ls *DefaultLoginService) RequireLoginWithPassword() (string, error) {
	fmt.Println("\n=== TChat 用户登录 ===")
	
	// 获取所有可用账户
	accounts := ls.accountManager.GetAllAccounts()
	
	// 如果没有账户，引导创建新账户
	if len(accounts) == 0 {
		fmt.Println("\n📝 系统中没有找到任何账户，请先创建一个新账户")
		err := ls.CreateNewAccount()
		if err != nil {
			return "", err
		}
		// 创建新账户后，需要重新获取账户列表并登录
		accounts = ls.accountManager.GetAllAccounts()
		if len(accounts) == 0 {
			return "", fmt.Errorf("创建账户后未找到账户")
		}
		// 直接登录新创建的账户
		return ls.LoginExistingAccountWithPassword(accounts)
	}
	
	// 显示登录选项
	fmt.Println("\n请选择登录方式:")
	fmt.Println("1. 选择现有账户登录")
	fmt.Println("2. 创建新账户")
	fmt.Print("\n请输入选项 (1-2): ")
	
	reader := bufio.NewReader(os.Stdin)
	choice, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取输入失败: %v", err)
	}
	
	choice = strings.TrimSpace(choice)
	switch choice {
	case "1":
		return ls.LoginExistingAccountWithPassword(accounts)
	case "2":
		err := ls.CreateNewAccount()
		if err != nil {
			return "", err
		}
		// 创建新账户后，需要重新获取账户列表并登录
		accounts = ls.accountManager.GetAllAccounts()
		if len(accounts) == 0 {
			return "", fmt.Errorf("创建账户后未找到账户")
		}
		// 直接登录新创建的账户
		return ls.LoginExistingAccountWithPassword(accounts)
	default:
		return "", fmt.Errorf("无效的选项: %s", choice)
	}
}

// LoginExistingAccount 登录现有账户
//
// 参数：
//   accounts - 可用账户列表
//
// 返回值：
//   error - 登录过程中的错误
//
// 处理逻辑：
//   1. 显示账户列表
//   2. 用户选择账户
//   3. 输入密码验证
//   4. 设置当前账户
func (ls *DefaultLoginService) LoginExistingAccount(accounts []*Account) error {
	_, err := ls.LoginExistingAccountWithPassword(accounts)
	return err
}

// LoginExistingAccountWithPassword 登录现有账户并返回密码
//
// 参数：
//   accounts - 可用账户列表
//
// 返回值：
//   string - 用户输入的密码
//   error - 登录过程中的错误
//
// 处理逻辑：
//   1. 显示账户列表
//   2. 用户选择账户
//   3. 输入密码验证
//   4. 设置当前账户
func (ls *DefaultLoginService) LoginExistingAccountWithPassword(accounts []*Account) (string, error) {
	fmt.Println("\n=== 选择账户 ===")
	
	// 显示账户列表
	for i, acc := range accounts {
		fmt.Printf("%d. %s\n", i+1, acc.Username)
	}
	
	fmt.Printf("\n请选择账户 (1-%d): ", len(accounts))
	
	reader := bufio.NewReader(os.Stdin)
	choiceStr, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取输入失败: %v", err)
	}
	
	choice, err := strconv.Atoi(strings.TrimSpace(choiceStr))
	if err != nil || choice < 1 || choice > len(accounts) {
		return "", fmt.Errorf("无效的账户选择: %s", choiceStr)
	}
	
	selectedAccount := accounts[choice-1]
	
	// 验证密码
	fmt.Printf("\n请输入账户 '%s' 的密码: ", selectedAccount.Username)
	password, err := ls.ReadPassword()
	if err != nil {
		return "", fmt.Errorf("读取密码失败: %v", err)
	}
	
	// 设置当前账户并解密公钥
	if err := ls.accountManager.SetCurrentAccountWithPassword(selectedAccount.Username, password); err != nil {
		return "", fmt.Errorf("登录失败: %v", err)
	}
	
	fmt.Printf("\n✅ 登录成功！欢迎回来，%s\n", selectedAccount.Username)
	return password, nil
}

// CreateNewAccount 创建新账户
//
// 返回值：
//   error - 创建过程中的错误
//
// 处理逻辑：
//   1. 输入用户名并验证唯一性
//   2. 输入密码并确认
//   3. 创建账户并设置为当前账户
func (ls *DefaultLoginService) CreateNewAccount() error {
	fmt.Println("\n=== 创建新账户 ===")
	
	reader := bufio.NewReader(os.Stdin)
	
	// 输入用户名
	fmt.Print("请输入用户名: ")
	username, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取用户名失败: %v", err)
	}
	username = strings.TrimSpace(username)
	
	if username == "" {
		return fmt.Errorf("用户名不能为空")
	}
	
	// 检查用户名是否已存在
	if ls.accountManager.GetAccount(username) != nil {
		return fmt.Errorf("用户名已存在: %s", username)
	}
	
	// 输入密码
	fmt.Print("请输入密码: ")
	password, err := ls.ReadPassword()
	if err != nil {
		return fmt.Errorf("读取密码失败: %v", err)
	}
	
	if len(password) < 6 {
		return fmt.Errorf("密码长度至少6位")
	}
	
	// 确认密码
	fmt.Print("请再次输入密码: ")
	confirmPassword, err := ls.ReadPassword()
	if err != nil {
		return fmt.Errorf("读取确认密码失败: %v", err)
	}
	
	if password != confirmPassword {
		return fmt.Errorf("两次输入的密码不一致")
	}
	
	// 创建账户
	fmt.Println("\n正在创建账户...")
	acc, err := ls.accountManager.CreateAccount(username, password)
	if err != nil {
		return fmt.Errorf("创建账户失败: %v", err)
	}
	
	fmt.Printf("\n✅ 账户创建成功！欢迎，%s\n", acc.Username)
	return nil
}

// ReadPassword 安全地读取密码（不显示在终端）
//
// 返回值：
//   string - 用户输入的密码
//   error - 读取过程中的错误
//
// 功能特性：
//   - 密码输入时不在终端显示
//   - 使用系统级安全输入机制
//   - 自动处理换行符
func (ls *DefaultLoginService) ReadPassword() (string, error) {
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Println() // 换行
	return string(bytePassword), nil
}