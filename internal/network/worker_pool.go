package network

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// WorkerPool 动态工作池
type WorkerPool struct {
	minWorkers    int
	maxWorkers    int
	currentWorkers int64
	workChan      chan func()
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	logger        Logger
	
	// 性能监控
	processedTasks int64
	queuedTasks    int64
	lastAdjustTime time.Time
	adjustMutex    sync.Mutex
}

// NewWorkerPool 创建动态工作池
func NewWorkerPool(minWorkers, maxWorkers, queueSize int, logger Logger) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	
	// 确保最小worker数量合理
	if minWorkers < 1 {
		minWorkers = 1
	}
	if maxWorkers < minWorkers {
		maxWorkers = minWorkers
	}
	
	// 根据CPU核心数调整默认值
	cpuCount := runtime.NumCPU()
	if maxWorkers > cpuCount*2 {
		maxWorkers = cpuCount * 2 // 限制最大worker数量
	}
	
	wp := &WorkerPool{
		minWorkers:     minWorkers,
		maxWorkers:     maxWorkers,
		currentWorkers: int64(minWorkers),
		workChan:       make(chan func(), queueSize),
		ctx:            ctx,
		cancel:         cancel,
		logger:         logger,
		lastAdjustTime: time.Now(),
	}
	
	return wp
}

// Start 启动工作池
func (wp *WorkerPool) Start() {
	// 启动最小数量的worker
	for i := 0; i < wp.minWorkers; i++ {
		wp.startWorker()
	}
	
	// 启动动态调整器
	go wp.startDynamicAdjuster()
}

// Stop 停止工作池
func (wp *WorkerPool) Stop() {
	wp.cancel()
	close(wp.workChan)
	wp.wg.Wait()
}

// Submit 提交任务到工作池
func (wp *WorkerPool) Submit(task func()) bool {
	atomic.AddInt64(&wp.queuedTasks, 1)
	
	select {
	case wp.workChan <- task:
		return true
	default:
		atomic.AddInt64(&wp.queuedTasks, -1)
		return false // 队列已满
	}
}

// startWorker 启动一个worker
func (wp *WorkerPool) startWorker() {
	wp.wg.Add(1)
	go func() {
		defer wp.wg.Done()
		defer atomic.AddInt64(&wp.currentWorkers, -1)
		
		for {
			select {
			case task := <-wp.workChan:
				if task != nil {
					task()
					atomic.AddInt64(&wp.processedTasks, 1)
					atomic.AddInt64(&wp.queuedTasks, -1)
				}
			case <-wp.ctx.Done():
				return
			}
		}
	}()
}

// startDynamicAdjuster 启动动态调整器
func (wp *WorkerPool) startDynamicAdjuster() {
	ticker := time.NewTicker(5 * time.Second) // 每5秒检查一次
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			wp.adjustWorkerCount()
		case <-wp.ctx.Done():
			return
		}
	}
}

// adjustWorkerCount 动态调整worker数量
func (wp *WorkerPool) adjustWorkerCount() {
	wp.adjustMutex.Lock()
	defer wp.adjustMutex.Unlock()
	
	// 防止频繁调整
	if time.Since(wp.lastAdjustTime) < 10*time.Second {
		return
	}
	
	currentWorkers := atomic.LoadInt64(&wp.currentWorkers)
	queueLength := len(wp.workChan)
	queueCapacity := cap(wp.workChan)
	
	// 计算队列使用率
	queueUsage := float64(queueLength) / float64(queueCapacity)
	
	// 根据队列使用率调整worker数量
	if queueUsage > 0.8 && currentWorkers < int64(wp.maxWorkers) {
		// 队列使用率高，增加worker
		newWorkers := int(currentWorkers) + 1
		if newWorkers <= wp.maxWorkers {
			wp.startWorker()
			atomic.AddInt64(&wp.currentWorkers, 1)
			wp.logger.Debugf("增加worker，当前数量: %d", newWorkers)
		}
	} else if queueUsage < 0.2 && currentWorkers > int64(wp.minWorkers) {
		// 队列使用率低，考虑减少worker（通过超时机制自然减少）
		// 这里不主动杀死worker，让它们自然超时退出
		wp.logger.Debugf("队列使用率低 (%.2f%%)，当前worker数量: %d", queueUsage*100, currentWorkers)
	}
	
	wp.lastAdjustTime = time.Now()
}

// GetStats 获取工作池统计信息
func (wp *WorkerPool) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"current_workers":  atomic.LoadInt64(&wp.currentWorkers),
		"min_workers":      wp.minWorkers,
		"max_workers":      wp.maxWorkers,
		"queue_length":     len(wp.workChan),
		"queue_capacity":   cap(wp.workChan),
		"queue_usage":      float64(len(wp.workChan)) / float64(cap(wp.workChan)),
		"processed_tasks":  atomic.LoadInt64(&wp.processedTasks),
		"queued_tasks":     atomic.LoadInt64(&wp.queuedTasks),
	}
}

// GetCurrentWorkerCount 获取当前worker数量
func (wp *WorkerPool) GetCurrentWorkerCount() int {
	return int(atomic.LoadInt64(&wp.currentWorkers))
}

// GetQueueLength 获取队列长度
func (wp *WorkerPool) GetQueueLength() int {
	return len(wp.workChan)
}

// GetQueueCapacity 获取队列容量
func (wp *WorkerPool) GetQueueCapacity() int {
	return cap(wp.workChan)
}

// IsOverloaded 检查是否过载
func (wp *WorkerPool) IsOverloaded() bool {
	queueUsage := float64(len(wp.workChan)) / float64(cap(wp.workChan))
	return queueUsage > 0.9 // 90%以上认为过载
}