package friend

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
)

// BlacklistManager 黑名单管理器
type Blacklist struct {
	Dir   string   // 存储目录
	Users []string // 被拉黑的用户名列
}

// NewBlacklistManager 创建黑名单管理器
func NewBlacklist(dir string) *Blacklist {
	return &Blacklist{Dir: dir}
}

// Load 加载黑名单
func (bl *Blacklist) Load() error {
	file := filepath.Join(bl.Dir, "blacklist.json")
	if _, err := os.Stat(file); os.IsNotExist(err) {
		bl.Users = []string{}
		return nil
	}
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &bl.Users)
}

// Save 保存黑名单
func (bl *Blacklist) Save() error {
	if err := os.MkdirAll(bl.Dir, 0700); err != nil {
		return err
	}
	file := filepath.Join(bl.Dir, "blacklist.json")
	data, err := json.MarshalIndent(bl.Users, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(file, data, 0600)
}

// AddToBlacklist 添加用户到黑名单
func (bl *Blacklist) Add(username string) error {
	for _, u := range bl.Users {
		if u == username {
			return nil // 已在黑名单
		}
	}
	bl.Users = append(bl.Users, username)
	return bl.Save()
}

// RemoveFromBlacklist 从黑名单移除用户
func (bl *Blacklist) Remove(username string) error {
	idx := -1
	for i, u := range bl.Users {
		if u == username {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil // 不在黑名单
	}
	bl.Users = append(bl.Users[:idx], bl.Users[idx+1:]...)
	return bl.Save()
}

// IsBlacklisted 判断用户是否在黑名单
func (bl *Blacklist) IsBlocked(username string) bool {
	for _, u := range bl.Users {
		if u == username {
			return true
		}
	}
	return false
}

// ListBlacklist 列出所有黑名单用户
