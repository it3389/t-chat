package router

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Trace 记录一次消息的路由路径
type Trace struct {
	MsgID     string    `json:"msg_id"`    // 消息唯一ID
	Path      []string  `json:"path"`      // 路由节点
	Timestamp time.Time `json:"timestamp"` // 路由时间
}

// TraceStore 路由追踪存储管理器
type TraceStore struct {
	Dir    string   // 存储目录
	Traces []*Trace // 路由追踪记录
	mutex  sync.Mutex
}

// NewTraceStore 创建路由追踪存储管理器
func NewTraceStore(dir string) *TraceStore {
	return &TraceStore{Dir: dir}
}

// Load 加载本地路由追踪记录
func (ts *TraceStore) Load() error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	file := filepath.Join(ts.Dir, "traces.json")
	if _, err := os.Stat(file); os.IsNotExist(err) {
		ts.Traces = []*Trace{}
		return nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &ts.Traces)
}

// Save 保存路由追踪记录到本地
func (ts *TraceStore) Save() error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	if err := os.MkdirAll(ts.Dir, 0700); err != nil {
		return err
	}
	file := filepath.Join(ts.Dir, "traces.json")
	data, err := json.MarshalIndent(ts.Traces, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0600)
}

// Add 记录一次新的路由追踪
func (ts *TraceStore) Add(trace *Trace) error {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	ts.Traces = append(ts.Traces, trace)
	return ts.Save()
}

// Query 按消息ID查询路由路径
func (ts *TraceStore) Query(msgID string) *Trace {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	for _, t := range ts.Traces {
		if t.MsgID == msgID {
			return t
		}
	}
	return nil
}

// List 返回所有路由追踪记录
func (ts *TraceStore) List() []*Trace {
	ts.mutex.Lock()
	defer ts.mutex.Unlock()
	return ts.Traces
}
