package file

import (
	"fmt"
	"time"
)

// ProgressBar 用于展示文件传输进度
type ProgressBar struct {
	Total      int       // 总分片数
	Completed  int       // 已完成分片数
	StartTime  time.Time // 传输开始时间
	LastUpdate time.Time // 上次更新时间
}

// NewProgressBar 创建进度条
func NewProgressBar(total int) *ProgressBar {
	return &ProgressBar{
		Total:     total,
		Completed: 0,
		StartTime: time.Now(),
	}
}

// Update 更新已完成分片数并显示进度
func (pb *ProgressBar) Update(completed int) {
	pb.Completed = completed
	pb.LastUpdate = time.Now()
	pb.Display()
}

// Display 显示当前进度、百分比、速度
func (pb *ProgressBar) Display() {
	var percent float64
	if pb.Total > 0 {
		percent = float64(pb.Completed) / float64(pb.Total) * 100
	}
	
	elapsed := time.Since(pb.StartTime).Seconds()
	var speed float64
	if elapsed > 0.1 { // 避免传输刚开始时的异常高速度
		speed = float64(pb.Completed) / elapsed // 分片/秒
	}
	
	fmt.Printf("\r进度: %d/%d (%.2f%%) 速度: %.2f 分片/秒", pb.Completed, pb.Total, percent, speed)
	if pb.Total > 0 && pb.Completed == pb.Total {
		fmt.Println("\n传输完成")
	}
}

// Logger 接口定义
type Logger interface {
	Infof(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

// Progress 传输进度管理器
type Progress struct {
	total     int
	completed int
	startTime time.Time
	logger    Logger
}

// NewProgress 创建进度管理器
func NewProgress(total int, logger Logger) *Progress {
	return &Progress{
		total:     total,
		completed: 0,
		startTime: time.Now(),
		logger:    logger,
	}
}

// Update 更新进度
func (p *Progress) Update(completed int) {
	p.completed = completed
	var percent float64
	if p.total > 0 {
		percent = float64(p.completed) / float64(p.total) * 100
	} else {
		percent = 0
	}
	elapsed := time.Since(p.startTime).Seconds()
	var speed float64
	if elapsed > 0.1 { // 避免传输刚开始时的异常高速度
		speed = float64(p.completed) / elapsed
	}

	if p.logger != nil {
		p.logger.Infof("传输进度更新: completed=%d, total=%d, percent=%.2f%%, speed=%.2f", 
			p.completed, p.total, percent, speed)
	}

	fmt.Printf("\r进度: %d/%d (%.2f%%) 速度: %.2f 分片/秒", p.completed, p.total, percent, speed)
	if p.total > 0 && p.completed == p.total {
		fmt.Println("\n传输完成")
	}
}

// GetProgress 获取当前进度
func (p *Progress) GetProgress() (completed, total int) {
	return p.completed, p.total
}

// GetPercent 获取进度百分比
func (p *Progress) GetPercent() float64 {
	if p.total == 0 {
		return 0
	}
	return float64(p.completed) / float64(p.total) * 100
}

// SetTotal 设置总分片数
func (p *Progress) SetTotal(total int) {
	p.total = total
}
