package network

import (
	"container/heap"
	"sync"
	"time"
)

// MessagePriority 消息优先级
type MessagePriority int

const (
	PriorityLow    MessagePriority = 1
	PriorityNormal MessagePriority = 2
	PriorityHigh   MessagePriority = 3
	PriorityCritical MessagePriority = 4
)

// PriorityMessage 带优先级的消息
type PriorityMessage struct {
	Message   *Message
	Priority  MessagePriority
	Timestamp time.Time
	Index     int // heap接口需要的索引
}

// PriorityQueue 优先级队列
type PriorityQueue struct {
	mutex    sync.RWMutex
	heap     priorityHeap
	maxSize  int
	dropped  int64
}

// priorityHeap 实现heap.Interface的优先级堆
type priorityHeap []*PriorityMessage

func (pq priorityHeap) Len() int { return len(pq) }

func (pq priorityHeap) Less(i, j int) bool {
	// 优先级高的排在前面，如果优先级相同则按时间排序（先进先出）
	if pq[i].Priority == pq[j].Priority {
		return pq[i].Timestamp.Before(pq[j].Timestamp)
	}
	return pq[i].Priority > pq[j].Priority
}

func (pq priorityHeap) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].Index = i
	pq[j].Index = j
}

func (pq *priorityHeap) Push(x interface{}) {
	n := len(*pq)
	item := x.(*PriorityMessage)
	item.Index = n
	*pq = append(*pq, item)
}

func (pq *priorityHeap) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // 避免内存泄漏
	item.Index = -1 // 标记为已移除
	*pq = old[0 : n-1]
	return item
}

// NewPriorityQueue 创建优先级队列
func NewPriorityQueue(maxSize int) *PriorityQueue {
	pq := &PriorityQueue{
		maxSize: maxSize,
	}
	heap.Init(&pq.heap)
	return pq
}

// Push 添加消息到优先级队列
func (pq *PriorityQueue) Push(msg *Message, priority MessagePriority) bool {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	
	// 如果队列已满，检查是否可以替换低优先级消息
	if len(pq.heap) >= pq.maxSize {
		// 如果新消息优先级高于队列中最低优先级的消息，则替换
		if len(pq.heap) > 0 {
			lowestPriorityMsg := pq.heap[len(pq.heap)-1]
			if priority > lowestPriorityMsg.Priority {
				// 移除最低优先级消息
				heap.Remove(&pq.heap, len(pq.heap)-1)
				pq.dropped++
			} else {
				// 新消息优先级不够高，丢弃
				pq.dropped++
				return false
			}
		} else {
			// 队列满且为空（不应该发生），丢弃消息
			pq.dropped++
			return false
		}
	}
	
	priorityMsg := &PriorityMessage{
		Message:   msg,
		Priority:  priority,
		Timestamp: time.Now(),
	}
	
	heap.Push(&pq.heap, priorityMsg)
	return true
}

// Pop 从优先级队列中取出最高优先级的消息
func (pq *PriorityQueue) Pop() *Message {
	pq.mutex.Lock()
	defer pq.mutex.Unlock()
	
	if len(pq.heap) == 0 {
		return nil
	}
	
	priorityMsg := heap.Pop(&pq.heap).(*PriorityMessage)
	return priorityMsg.Message
}

// Len 返回队列长度
func (pq *PriorityQueue) Len() int {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	return len(pq.heap)
}

// Cap 返回队列容量
func (pq *PriorityQueue) Cap() int {
	return pq.maxSize
}

// DroppedCount 返回丢弃的消息数量
func (pq *PriorityQueue) DroppedCount() int64 {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	return pq.dropped
}

// GetMessagePriority 根据消息类型确定优先级
func GetMessagePriority(msg *Message) MessagePriority {
	switch msg.Type {
	case "system", "error", "ack":
		return PriorityCritical
	case "user_info_exchange", "ping", "pong":
		return PriorityHigh
	case "file":
		return PriorityNormal
	case "text":
		return PriorityNormal
	default:
		return PriorityLow
	}
}

// IsHighLoad 检查是否处于高负载状态
func (pq *PriorityQueue) IsHighLoad() bool {
	pq.mutex.RLock()
	defer pq.mutex.RUnlock()
	
	// 当队列使用率超过80%时认为是高负载
	usageRate := float64(len(pq.heap)) / float64(pq.maxSize)
	return usageRate > 0.8
}