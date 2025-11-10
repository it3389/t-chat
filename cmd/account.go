// Package cmd 提供t-chat应用程序的命令行界面
//
// 账户管理命令模块负责处理用户账户相关的所有操作，包括：
// - 账户创建和删除
// - 账户列表查看
// - 账户选择和切换
// - 账户信息查看
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"t-chat/internal/account"

	"github.com/spf13/cobra"
)

// AccountCommandHandler 账户命令处理器
//
// 核心功能：
// - 处理所有账户相关的CLI命令
// - 提供用户交互界面
// - 管理账户操作的业务逻辑
//
// 设计目标：
// - 封装账户命令的处理逻辑
// - 提供清晰的用户交互体验
// - 确保输入验证和错误处理
type AccountCommandHandler struct {
	accountMgr *account.Manager
}

// NewAccountCommandHandler 创建账户命令处理器
//
// 参数：
//   mgr - 账户管理器实例
//
// 返回值：
//   *AccountCommandHandler - 账户命令处理器实例
func NewAccountCommandHandler(mgr *account.Manager) *AccountCommandHandler {
	return &AccountCommandHandler{
		accountMgr: mgr,
	}
}

// readUserInput 读取用户输入
//
// 参数：
//   prompt - 提示信息
//
// 返回值：
//   string - 用户输入的字符串（已去除首尾空格）
//
// 处理逻辑：
//   1. 显示提示信息
//   2. 读取用户输入
//   3. 去除首尾空格并返回
func (h *AccountCommandHandler) readUserInput(prompt string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// handleCreateAccount 处理创建账户命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数
//
// 处理逻辑：
//   1. 获取用户名和密码输入（支持命令行参数或交互式输入）
//   2. 创建账户对象
//   3. 调用账户管理器创建账户
//   4. 验证公钥解密
//   5. 输出创建结果
//
// 异常情况：
//   - 用户名为空：提示用户重新输入
//   - 密码为空：提示用户重新输入
//   - 账户创建失败：显示错误信息
//   - 公钥解密失败：显示错误信息
func (h *AccountCommandHandler) handleCreateAccount(cmd *cobra.Command, args []string) {
	// 尝试从命令行参数获取用户名和密码
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")
	
	// 如果命令行参数为空，则使用交互式输入
	if username == "" {
		username = h.readUserInput("请输入用户名: ")
		if username == "" {
			fmt.Println("用户名不能为空")
			return
		}
	}

	if password == "" {
		password = h.readUserInput("请输入账户密码（用于加密私钥）: ")
		if password == "" {
			fmt.Println("密码不能为空")
			return
		}
	}

	// 调用账户管理器创建账户
	acc, err := h.accountMgr.CreateAccount(username, password)
	if err != nil {
		fmt.Printf("账户创建失败: %v\n", err)
		return
	}

	// 验证公钥解密是否正常，确保账户可用
	_, _, err = h.accountMgr.DecryptAccountKeys(acc, password)
	if err != nil {
		fmt.Printf("公钥解密失败: %v\n", err)
		return
	}

	fmt.Printf("账户创建成功，用户名: %s\n", acc.Username)
}

// handleListAccounts 处理列出所有账户命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数
//
// 处理逻辑：
//   1. 获取所有账户列表
//   2. 格式化输出账户信息
//
// 输出格式：
//   用户名: [username], 创建时间: [yyyy-mm-dd hh:mm:ss]
// handleListAccounts 处理列出所有账户命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数（此命令不需要参数）
//
// 处理逻辑：
//   1. 获取所有账户列表
//   2. 检查账户数量
//   3. 格式化输出账户信息
//
// 输出格式：
//   - 无账户时：显示"暂无账户"
//   - 有账户时：显示用户名和创建时间
func (h *AccountCommandHandler) handleListAccounts(cmd *cobra.Command, args []string) {
	// 获取所有账户列表
	accounts := h.accountMgr.GetAllAccounts()
	
	if len(accounts) == 0 {
		fmt.Println("暂无账户")
		return
	}

	fmt.Println("账户列表：")
	for _, acc := range accounts {
		fmt.Printf("用户名: %s, 创建时间: %s\n", 
			acc.Username, 
			acc.CreatedAt.Format("2006-01-02 15:04:05"))
	}
}

// handleDeleteAccount 处理删除账户命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数（第一个参数为用户名）
//
// 处理逻辑：
//   1. 验证参数有效性
//   2. 调用账户管理器删除账户
//   3. 输出删除结果
//
// 异常情况：
//   - 未提供用户名：提示用户提供用户名
//   - 删除失败：显示错误信息
func (h *AccountCommandHandler) handleDeleteAccount(cmd *cobra.Command, args []string) {
	// 验证参数，确保提供了用户名
	if len(args) < 1 {
		fmt.Println("请指定要删除的用户名")
		fmt.Println("使用方式: tchat account delete <用户名>")
		return
	}

	username := args[0]
	
	// 调用账户管理器删除账户
	err := h.accountMgr.DeleteAccount(username)
	if err != nil {
		fmt.Printf("账户删除失败: %v\n", err)
		return
	}

	fmt.Printf("账户删除成功，用户名: %s\n", username)
}

// handleSelectAccount 处理选择当前账户命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数（第一个参数为用户名）
//
// 处理逻辑：
//   1. 验证参数有效性
//   2. 调用账户管理器设置当前账户
//   3. 输出切换结果
//
// 异常情况：
//   - 未提供用户名：提示用户提供用户名
//   - 切换失败：显示错误信息
func (h *AccountCommandHandler) handleSelectAccount(cmd *cobra.Command, args []string) {
	// 验证参数，确保提供了用户名
	if len(args) < 1 {
		fmt.Println("请指定要选择的用户名")
		fmt.Println("使用方式: tchat account select <用户名>")
		return
	}

	username := args[0]
	
	// 调用账户管理器设置当前账户
	err := h.accountMgr.SetCurrentAccount(username)
	if err != nil {
		fmt.Printf("账户选择失败: %v\n", err)
		return
	}

	fmt.Printf("当前账户已切换为用户名: %s\n", username)
}

// handleAccountInfo 处理查看当前账户详情命令
//
// 参数：
//   cmd - cobra命令对象
//   args - 命令行参数
//
// 处理逻辑：
//   1. 获取当前账户信息
//   2. 尝试解密公钥
//   3. 格式化输出账户详情
//
// 异常情况：
//   - 未选择账户：提示用户先选择账户
//   - 公钥解密失败：显示解密失败信息
func (h *AccountCommandHandler) handleAccountInfo(cmd *cobra.Command, args []string) {
	// 获取当前账户信息
	acc := h.accountMgr.GetCurrentAccount()
	if acc == nil {
		fmt.Println("未选择账户")
		fmt.Println("请先使用 'tchat account select <用户名>' 选择账户")
		return
	}

	fmt.Println("当前账户详情：")
	fmt.Printf("用户名: %s\n", acc.Username)
	fmt.Printf("创建时间: %s\n", acc.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("密码哈希: %s...\n", acc.PasswordHash[:16])
	
	// 直接显示明文公钥（已在登录时解密）
	if acc.PublicKey != "" {
		fmt.Printf("公钥: %s\n", acc.PublicKey)
	} else {
		fmt.Println("公钥: 未解密（请重新登录）")
	}
}

// 全局账户命令处理器实例
var accountHandler *AccountCommandHandler

// initAccountCommands 初始化账户相关命令
//
// 处理逻辑：
//   1. 创建账户命令处理器实例
//   2. 定义所有账户相关的cobra命令
//   3. 设置命令处理函数
//   4. 注册命令到根命令
func initAccountCommands() {
	// 创建账户命令处理器（延迟初始化，在SetAccountManager时设置）
	
	// 定义账户管理主命令
	accountCmd := &cobra.Command{
		Use:   "account",
		Short: "账户管理相关命令",
		Long: `账户管理命令提供完整的用户账户生命周期管理功能，包括：
- 创建新账户并生成密钥对
- 列出所有已创建的账户
- 删除不需要的账户
- 选择和切换当前使用的账户
- 查看当前账户的详细信息`,
	}

	// 定义创建账户子命令
	createAccountCmd := &cobra.Command{
		Use:   "create",
		Short: "创建新账户",
		Long: `创建新的用户账户，包括：
- 生成Ed25519密钥对
- 使用密码加密私钥
- 保存账户信息到本地文件`,
		Run: func(cmd *cobra.Command, args []string) {
			if accountHandler != nil {
				accountHandler.handleCreateAccount(cmd, args)
			} else {
				fmt.Println("账户管理器未初始化")
			}
		},
	}
	
	// 添加命令行参数
	createAccountCmd.Flags().String("username", "", "用户名")
	createAccountCmd.Flags().String("password", "", "密码")

	// 定义列出账户子命令
	listAccountCmd := &cobra.Command{
		Use:   "list",
		Short: "列出所有账户",
		Long:  `显示所有已创建的账户列表，包括用户名和创建时间`,
		Run: func(cmd *cobra.Command, args []string) {
			if accountHandler != nil {
				accountHandler.handleListAccounts(cmd, args)
			} else {
				fmt.Println("账户管理器未初始化")
			}
		},
	}

	// 定义删除账户子命令
	deleteAccountCmd := &cobra.Command{
		Use:   "delete <用户名>",
		Short: "删除指定账户",
		Long: `删除指定的用户账户，包括：
- 删除账户文件
- 清理相关的密钥信息
- 如果是当前账户，则清除当前账户设置`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if accountHandler != nil {
				accountHandler.handleDeleteAccount(cmd, args)
			} else {
				fmt.Println("账户管理器未初始化")
			}
		},
	}

	// 定义选择账户子命令
	selectAccountCmd := &cobra.Command{
		Use:   "select <用户名>",
		Short: "选择当前账户",
		Long: `设置指定账户为当前使用的账户，后续的聊天和文件传输操作将使用该账户的身份`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if accountHandler != nil {
				accountHandler.handleSelectAccount(cmd, args)
			} else {
				fmt.Println("账户管理器未初始化")
			}
		},
	}

	// 定义查看账户信息子命令
	infoAccountCmd := &cobra.Command{
		Use:   "info",
		Short: "查看当前账户详情",
		Long: `显示当前选择账户的详细信息，包括：
- 用户名
- 创建时间
- 公钥信息（用于身份识别）`,
		Run: func(cmd *cobra.Command, args []string) {
			if accountHandler != nil {
				accountHandler.handleAccountInfo(cmd, args)
			} else {
				fmt.Println("账户管理器未初始化")
			}
		},
	}

	// 注册子命令到主命令
	accountCmd.AddCommand(createAccountCmd)
	accountCmd.AddCommand(listAccountCmd)
	accountCmd.AddCommand(deleteAccountCmd)
	accountCmd.AddCommand(selectAccountCmd)
	accountCmd.AddCommand(infoAccountCmd)

	// 添加帮助标志和处理器

	
	// 注册主命令到根命令
	RootCmd.AddCommand(accountCmd)
}

// SetAccountManagerForAccountCmd 设置账户命令的账户管理器
//
// 参数：
//   mgr - 账户管理器实例
//
// 处理逻辑：
//   1. 创建账户命令处理器实例
//   2. 设置账户管理器依赖
func SetAccountManagerForAccountCmd(mgr *account.Manager) {
	accountHandler = NewAccountCommandHandler(mgr)
}

func init() {
	// 初始化账户相关命令
	initAccountCommands()
}
