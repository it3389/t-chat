package network

import (
	"fmt"
	"time"
)

// MetricsMonitor 监控指标监视器
type MetricsMonitor struct {
	pineconeService *PineconeService
	logger          Logger
	isRunning       bool
	stopChan        chan struct{}
}

// NewMetricsMonitor 创建监控指标监视器
func NewMetricsMonitor(ps *PineconeService, logger Logger) *MetricsMonitor {
	return &MetricsMonitor{
		pineconeService: ps,
		logger:          logger,
		stopChan:        make(chan struct{}),
	}
}

// Start 启动监控
func (mm *MetricsMonitor) Start(interval time.Duration) {
	if mm.isRunning {
		return
	}
	
	mm.isRunning = true
	go mm.monitorLoop(interval)
}

// Stop 停止监控
func (mm *MetricsMonitor) Stop() {
	if !mm.isRunning {
		return
	}
	
	mm.isRunning = false
	close(mm.stopChan)
}

// monitorLoop 监控循环
func (mm *MetricsMonitor) monitorLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			mm.logMetrics()
		case <-mm.stopChan:
			return
		}
	}
}

// logMetrics 记录监控指标
func (mm *MetricsMonitor) logMetrics() {
	stats := mm.pineconeService.GetMetricsStats()
	
	// 格式化输出监控信息
	mm.logger.Infof("📊 消息队列监控 - 处理: %d, 丢弃: %d, 丢弃率: %.2f%%, 队列使用率: %.2f%% (%d/%d)",
		stats["message_processed_count"],
		stats["message_drop_count"],
		stats["message_drop_rate"].(float64)*100,
		stats["queue_usage_rate"].(float64)*100,
		stats["queue_current_size"],
		stats["queue_capacity"])
	
	// 输出WorkerPool监控信息
	if workerPoolStats, ok := stats["worker_pool"].(map[string]interface{}); ok {
		mm.logger.Infof("🔧 工作池监控 - 当前Worker: %v, 队列使用率: %.2f%%, 过载状态: %v",
			workerPoolStats["current_workers"],
			stats["worker_pool_queue_usage"].(float64)*100,
			stats["worker_pool_overloaded"])
	}
	
	// 背压控制器监控信息已移除
}

// GetCurrentStats 获取当前统计信息
func (mm *MetricsMonitor) GetCurrentStats() string {
	stats := mm.pineconeService.GetMetricsStats()
	
	baseStats := fmt.Sprintf("消息统计 - 处理: %d, 丢弃: %d, 丢弃率: %.2f%%, 队列使用率: %.2f%% (%d/%d), 运行时间: %.1f秒",
		stats["message_processed_count"],
		stats["message_drop_count"],
		stats["message_drop_rate"].(float64)*100,
		stats["queue_usage_rate"].(float64)*100,
		stats["queue_current_size"],
		stats["queue_capacity"],
		stats["uptime_seconds"])
	
	// 添加WorkerPool统计信息
	if workerPoolStats, ok := stats["worker_pool"].(map[string]interface{}); ok {
		workerPoolInfo := fmt.Sprintf(", 工作池 - Worker: %v/%v/%v, 队列: %.1f%%, 过载: %v",
			workerPoolStats["current_workers"],
			workerPoolStats["min_workers"],
			workerPoolStats["max_workers"],
			stats["worker_pool_queue_usage"].(float64)*100,
			stats["worker_pool_overloaded"])
		
		// 背压控制器统计信息已移除
		
		return baseStats + workerPoolInfo
	}
	
	return baseStats
}

// CheckHealthStatus 检查健康状态
func (mm *MetricsMonitor) CheckHealthStatus() (bool, string) {
	stats := mm.pineconeService.GetMetricsStats()
	
	dropRate := stats["message_drop_rate"].(float64)
	queueUsage := stats["queue_usage_rate"].(float64)
	
	// 健康状态检查阈值
	const (
		maxDropRate   = 0.05  // 5% 最大丢弃率
		maxQueueUsage = 0.80  // 80% 最大队列使用率
		maxWorkerPoolQueueUsage = 0.85  // 85% 最大工作池队列使用率
	)
	
	if dropRate > maxDropRate {
		return false, fmt.Sprintf("消息丢弃率过高: %.2f%% (阈值: %.2f%%)", dropRate*100, maxDropRate*100)
	}
	
	if queueUsage > maxQueueUsage {
		return false, fmt.Sprintf("队列使用率过高: %.2f%% (阈值: %.2f%%)", queueUsage*100, maxQueueUsage*100)
	}
	
	// 检查WorkerPool健康状态
	if workerPoolQueueUsage, ok := stats["worker_pool_queue_usage"].(float64); ok {
		if workerPoolQueueUsage > maxWorkerPoolQueueUsage {
			return false, fmt.Sprintf("工作池队列使用率过高: %.2f%% (阈值: %.2f%%)", workerPoolQueueUsage*100, maxWorkerPoolQueueUsage*100)
		}
	}
	
	if workerPoolOverloaded, ok := stats["worker_pool_overloaded"].(bool); ok && workerPoolOverloaded {
		return false, "工作池处于过载状态"
	}
	
	// 背压控制器健康状态检查已移除
	
	return true, "系统运行正常"
}