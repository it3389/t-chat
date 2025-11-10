package cmd

import (
	"fmt"
	"os"

	"t-chat/internal/account"

	"github.com/spf13/cobra"
)

var (
	cfgFile      string
	logLevel     string
	username     string
	password     string
	accountName  string
	accountMgr   *account.Manager
)

// SetAccountManager 设置账户管理器
func SetAccountManager(am *account.Manager) {
	accountMgr = am
	account.SetGlobalManager(am)
	SetAccountManagerForAccountCmd(am)
}

// GetLoginParams 获取登录参数
func GetLoginParams() (string, string, string) {
	return username, password, cfgFile
}

// GetAccountName 获取账户名参数
func GetAccountName() string {
	return accountName
}



// RootCmd CLI 的根命令，由 main.go 调用
var RootCmd = &cobra.Command{
	Use:   "tchat",
	Short: "TChat - 完夫妻去中心化CLI聊天工具",
	Long:  `TChat 是完全去中心化 CLI 聊天系统。`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// 处理全局参数（避免在终端输出可能敏感的信息，如配置文件路径）
		// 如需查看详细启动信息，请使用 --log-level=debug
		if logLevel == "debug" {
			fmt.Printf("[TChat] 日志级别: %s\n", logLevel)
		}
	},
}

// Execute 启动 CLI
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "命令执行失败: %v\n", err)
		os.Exit(1)
	}
}





func init() {
	// 注册全局参数
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "配置文件路径 (可选)")
	RootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "日志级别 (debug|info|warn|error)")
	RootCmd.PersistentFlags().StringVar(&username, "username", "", "自动登录用户名 (可选)")
	RootCmd.PersistentFlags().StringVar(&password, "password", "", "自动登录密码 (可选)")
	RootCmd.PersistentFlags().StringVar(&accountName, "account", "", "指定要使用的账户名 (可选)")



	// 注册子命令
	// 注意：子命令在各自的文件中通过 init() 函数注册
	// 这里不需要手动添加，因为 Go 会自动调用所有包的 init() 函数
}
