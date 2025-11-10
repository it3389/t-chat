package friend

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Friend 好友结构
type Friend struct {
	Username     string    `json:"username"`
	PineconeAddr string    `json:"pinecone_addr"`
	IsOnline     bool      `json:"is_online"`
	LastSeen     time.Time `json:"last_seen"`
	AddedAt      time.Time `json:"added_at"`
}

// List 好友列表
type List struct {
	friends         map[string]*Friend
	mutex           sync.RWMutex
	filePath        string
	currentUsername string // 当前登录账户用户名
}

// FriendList 好友列表管理器

// NewList 创建新的好友列表
func NewList() *List {
	l := &List{
		friends:  make(map[string]*Friend),
		filePath: "friends/friends.json",
	}

	// 确保目录存在
	os.MkdirAll("friends", 0755)

	// 加载现有好友
	l.loadFriends()

	return l
}

// NewFriendList 创建好友列表管理器

// AddFriend 添加好友
func (l *List) AddFriend(friend *Friend) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if _, exists := l.friends[friend.Username]; exists {
		return fmt.Errorf("好友已存在 %s", friend.Username)
	}

	l.friends[friend.Username] = friend

	return l.saveFriends()
}

// GetFriend 获取好友
func (l *List) GetFriend(username string) *Friend {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	return l.friends[username]
}

// GetAllFriends 获取所有好友
func (l *List) GetAllFriends() []*Friend {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	friends := make([]*Friend, 0, len(l.friends))
	for _, f := range l.friends {
		friends = append(friends, f)
	}

	return friends
}

// RemoveFriend 删除好友
func (l *List) RemoveFriend(username string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if _, exists := l.friends[username]; !exists {
		return fmt.Errorf("好友不存在 %s", username)
	}

	delete(l.friends, username)

	return l.saveFriends()
}

// UpdateFriendStatus 更新好友状态
func (l *List) UpdateFriendStatus(username string, isOnline bool) {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	if friend, exists := l.friends[username]; exists {
		friend.IsOnline = isOnline
		friend.LastSeen = time.Now()
	}
}

// CleanupOfflineStatus 清理离线状态
func (l *List) CleanupOfflineStatus() {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	now := time.Now()
	timeout := 5 * time.Minute // 5分钟超时

	for _, friend := range l.friends {
		if friend.IsOnline && now.Sub(friend.LastSeen) > timeout {
			friend.IsOnline = false
		}
	}
}

// loadFriends 加载好友
func (l *List) loadFriends() error {
	data, err := os.ReadFile(l.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在是正常
		}
		return err
	}

	var friends []*Friend
	if err := json.Unmarshal(data, &friends); err != nil {
		return err
	}

	for _, f := range friends {
		l.friends[f.Username] = f
	}

	return nil
}

// saveFriends 保存好友
func (l *List) saveFriends() error {
	friends := l.GetAllFriends()

	data, err := json.MarshalIndent(friends, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(l.filePath, data, 0644)
}

// ListFriends 列出所有好友

// GetPineconeAddr 获取好友的 Pinecone 地址
func (l *List) GetPineconeAddr(username string) (string, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	f, ok := l.friends[username]
	if !ok {
		return "", fmt.Errorf("好友不存在: %s", username)
	}
	return f.PineconeAddr, nil
}

// GetPublicKey 获取好友的公钥（如无公钥字段，返回空字符串）
func (l *List) GetPublicKey(username string) (string, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	// Friend结构体无公钥字段，返回空字符串
	return "", nil
}

// SetCurrentAccount 设置当前登录账户用户名
func (l *List) SetCurrentAccount(username string) {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.currentUsername = username
}

func (l *List) GetCurrentAccount() *Friend {
	l.mutex.RLock()
	defer l.mutex.RUnlock()
	if l.currentUsername == "" {
		return nil
	}
	// 直接返回当前用户信息，而不是从好友列表中查找
	return &Friend{
		Username: l.currentUsername,
		// 其他字段保持默认值
	}
}
