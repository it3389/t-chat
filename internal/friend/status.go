package friend

import (
	"sync"
	"time"
)

// FriendStatus 好友在线状态管理器
type FriendStatus struct {
	Online    bool          // 是否在线
	UpdatedAt time.Time     // 最后更新时间
	TTL       time.Duration // 状态有效期
}

// StatusCache 管理所有好友的在线状态，支持 TTL 自动过期
type StatusCache struct {
	cache map[string]*FriendStatus // 用户-> 状态
	mutex sync.Mutex
}

// NewStatusCache 创建状态缓存
func NewStatusCache() *StatusCache {
	return &StatusCache{
		cache: make(map[string]*FriendStatus),
	}
}

// Set 设置好友状态及 TTL
func (sc *StatusCache) Set(username string, online bool, ttl time.Duration) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	sc.cache[username] = &FriendStatus{
		Online:    online,
		UpdatedAt: time.Now(),
		TTL:       ttl,
	}
}

// Get 查询好友状态，自动处理过期
func (sc *StatusCache) Get(username string) (online bool, ok bool) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	status, exists := sc.cache[username]
	if !exists {
		return false, false
	}
	if time.Since(status.UpdatedAt) > status.TTL {
		delete(sc.cache, username)
		return false, false
	}
	return status.Online, true
}

// Cleanup 清理所有已过期状态
func (sc *StatusCache) Cleanup() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	now := time.Now()
	for user, status := range sc.cache {
		if now.Sub(status.UpdatedAt) > status.TTL {
			delete(sc.cache, user)
		}
	}
}

// SetOnline 设置好友在线
func (sc *StatusCache) SetOnline(username string) {
	sc.Set(username, true, 0)
}

// SetOffline 设置好友离线
func (sc *StatusCache) SetOffline(username string) {
	sc.Set(username, false, 0)
}

// IsOnline 判断好友是否在线
func (sc *StatusCache) IsOnline(username string) bool {
	online, ok := sc.Get(username)
	return ok && online
}
