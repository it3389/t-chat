package network

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// NetworkService 网络服务统一实现
// 实现 NetworkServiceInterface 接口
type NetworkService struct {
	name      string
	config    *NetworkConfig
	running   bool
	startTime time.Time
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	logger    Logger
}

// 确保 NetworkService 实现了 NetworkServiceInterface 接口
var _ NetworkServiceInterface = (*NetworkService)(nil)

// NewNetworkService 创建网络服务
func NewNetworkService(name string, config *NetworkConfig, logger Logger) *NetworkService {
	if config == nil {
		config = DefaultNetworkConfig()
	}
	
	return &NetworkService{
		name:   name,
		config: config,
		logger: logger,
	}
}

// Start 启动服务
func (ns *NetworkService) Start(ctx context.Context) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	
	if ns.running {
		return fmt.Errorf("服务 %s 已经在运行", ns.name)
	}
	
	ns.ctx, ns.cancel = context.WithCancel(ctx)
	ns.running = true
	ns.startTime = time.Now()
	
	// 网络服务已启动
	
	return nil
}

// Stop 停止服务
func (ns *NetworkService) Stop() error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	
	if !ns.running {
		return fmt.Errorf("服务 %s 未运行", ns.name)
	}
	
	if ns.cancel != nil {
		ns.cancel()
	}
	
	ns.running = false
	
	// 网络服务已停止
	
	return nil
}

// IsRunning 检查服务是否运行
func (ns *NetworkService) IsRunning() bool {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.running
}

// GetConfig 获取配置
func (ns *NetworkService) GetConfig() *NetworkConfig {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.config
}

// SetConfig 设置配置
func (ns *NetworkService) SetConfig(config *NetworkConfig) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	
	if ns.running {
		return fmt.Errorf("无法在服务运行时修改配置")
	}
	
	ns.config = config
	return nil
}

// GetName 获取服务名称
func (ns *NetworkService) GetName() string {
	return ns.name
}

// GetStartTime 获取启动时间
func (ns *NetworkService) GetStartTime() time.Time {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.startTime
}

// GetContext 获取上下文
func (ns *NetworkService) GetContext() context.Context {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	return ns.ctx
}

// ServiceManager 改进的服务管理器
// 实现 ServiceManagerInterface 接口
type ServiceManager struct {
	*NetworkService
	services map[string]NetworkServiceInterface
	mu       sync.RWMutex
}

// 确保 ServiceManager 实现了 ServiceManagerInterface 接口
var _ ServiceManagerInterface = (*ServiceManager)(nil)

// NewServiceManager 创建服务管理器
func NewServiceManager(config *NetworkConfig, logger Logger) *ServiceManager {
	baseService := NewNetworkService("ServiceManager", config, logger)
	
	return &ServiceManager{
		NetworkService: baseService,
		services:       make(map[string]NetworkServiceInterface),
	}
}

// RegisterService 注册服务
func (sm *ServiceManager) RegisterService(name string, service NetworkServiceInterface) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if _, exists := sm.services[name]; exists {
		return fmt.Errorf("服务 %s 已存在", name)
	}
	
	sm.services[name] = service
	
	if sm.logger != nil {
		// 服务已注册
	}
	
	return nil
}

// UnregisterService 注销服务
func (sm *ServiceManager) UnregisterService(name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	service, exists := sm.services[name]
	if !exists {
		return fmt.Errorf("服务 %s 不存在", name)
	}
	
	// 停止服务
	if service.IsRunning() {
		if err := service.Stop(); err != nil {
			return fmt.Errorf("停止服务 %s 失败: %v", name, err)
		}
	}
	
	delete(sm.services, name)
	
	if sm.logger != nil {
		// 服务已注销
	}
	
	return nil
}

// GetService 获取服务
func (sm *ServiceManager) GetService(name string) (NetworkServiceInterface, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	service, exists := sm.services[name]
	return service, exists
}

// GetAllServices 获取所有服务
func (sm *ServiceManager) GetAllServices() map[string]NetworkServiceInterface {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	result := make(map[string]NetworkServiceInterface)
	for name, service := range sm.services {
		result[name] = service
	}
	
	return result
}

// StartService 启动指定服务
func (sm *ServiceManager) StartService(name string) error {
	sm.mu.RLock()
	service, exists := sm.services[name]
	sm.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("服务 %s 不存在", name)
	}
	
	return service.Start(sm.GetContext())
}

// StopService 停止指定服务
func (sm *ServiceManager) StopService(name string) error {
	sm.mu.RLock()
	service, exists := sm.services[name]
	sm.mu.RUnlock()
	
	if !exists {
		return fmt.Errorf("服务 %s 不存在", name)
	}
	
	return service.Stop()
}

// Start 启动服务管理器和所有注册的服务
func (sm *ServiceManager) Start(ctx context.Context) error {
	// 启动基础服务
	if err := sm.NetworkService.Start(ctx); err != nil {
		return err
	}
	
	// 启动所有注册的服务
	sm.mu.RLock()
	services := make(map[string]NetworkServiceInterface)
	for name, service := range sm.services {
		services[name] = service
	}
	sm.mu.RUnlock()

	for name, service := range services {
		if sm.logger != nil {
			// 正在启动服务
		}
		
		// 添加启动超时检测
		startChan := make(chan error, 1)
		go func() {
			startChan <- service.Start(sm.GetContext())
		}()
		
		select {
		case err := <-startChan:
			if err != nil {
				if sm.logger != nil {
					sm.logger.Errorf("❌ 启动服务 %s 失败: %v", name, err)
				}
				// 继续启动其他服务，不因为一个服务失败而停止
			} else {
				if sm.logger != nil {
					// 服务启动成功
				}
			}
		case <-time.After(10 * time.Second):
			if sm.logger != nil {
				sm.logger.Errorf("⏰ 服务 %s 启动超时 (10秒)", name)
			}
			// 继续启动其他服务，不因为一个服务超时而停止
		}
	}
	
	return nil
}

// Stop 停止服务管理器和所有服务
func (sm *ServiceManager) Stop() error {
	// 停止所有注册的服务
	sm.mu.RLock()
	services := make(map[string]NetworkServiceInterface)
	for name, service := range sm.services {
		services[name] = service
	}
	sm.mu.RUnlock()
	
	for name, service := range services {
		if service.IsRunning() {
			if err := service.Stop(); err != nil {
				if sm.logger != nil {
					sm.logger.Errorf("停止服务 %s 失败: %v", name, err)
				}
			}
		}
	}
	
	// 停止基础服务
	return sm.NetworkService.Stop()
}

// GetServiceStatus 获取所有服务状态
func (sm *ServiceManager) GetServiceStatus() map[string]bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	status := make(map[string]bool)
	for name, service := range sm.services {
		status[name] = service.IsRunning()
	}
	
	return status
}