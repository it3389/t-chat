# 文件传输模块架构

## 概述

文件传输模块采用分层架构设计，实现了完整的文件传输功能，包括断点续传、滑动窗口、自适应分片大小、ACK确认等特性。

## 模块结构

### 1. 协议层 (protocol.go)
- **功能**: 定义传输协议和消息格式
- **组件**:
  - `ProtocolHandler`: 协议处理器，负责消息编码/解码
  - `TransferRequest`: 传输请求结构
  - `TransferResponse`: 传输响应结构
  - `ChunkAck`: 分片确认结构
  - `TransferComplete`: 传输完成消息结构

### 2. 窗口层 (window.go)
- **功能**: 滑动窗口和流量控制
- **组件**:
  - `WindowConfig`: 窗口配置（窗口大小、超时时间、重试次数等）
  - `SlidingWindow`: 滑动窗口实现，管理已发送和已确认的分片

### 3. 会话层 (session.go)
- **功能**: 管理传输会话和状态
- **组件**:
  - `TransferSession`: 传输会话，包含窗口、进度、状态等信息
  - 会话状态管理（idle, starting, running, paused, complete, failed, canceled）

### 4. 分片层 (chunk.go)
- **功能**: 文件分片和重组
- **组件**:
  - `Chunk`: 分片结构，包含序号、偏移、数据、校验和等
  - `ChunkManager`: 分片管理器，负责文件分片的读取和传输
  - 分片校验和验证功能

### 5. 传输层 (transmission.go)
- **功能**: 核心传输逻辑和网络通信
- **组件**:
  - `TransmissionManager`: 传输管理器，实现核心传输逻辑
  - 自适应分片大小算法
  - 断点续传检查
  - ACK确认机制
  - 网络通信协议

### 6. 进度层 (progress.go)
- **功能**: 进度管理和显示
- **组件**:
  - `Progress`: 传输进度管理器
  - `ProgressBar`: 进度条显示

### 7. 接收层 (receiver.go)
- **功能**: 文件接收和重组
- **组件**:
  - `Receiver`: 文件接收器，负责接收分片并重组为完整文件
  - 断点续传支持

### 8. 管理层 (manager.go)
- **功能**: 文件管理
- **组件**:
  - `Manager`: 文件管理器，负责文件的保存、获取、删除等操作
  - `FileInfo`: 文件信息结构

### 9. 主入口 (transfer.go)
- **功能**: 主入口，提供统一的API接口
- **组件**:
  - `TransferManager`: 主传输管理器，委托给各个子模块

## 核心特性

### 1. 断点续传
- 支持传输中断后从断点继续
- 自动检测已接收的分片
- 发送方和接收方同步断点信息

### 2. 滑动窗口
- 实现流量控制
- 可配置窗口大小
- 支持窗口滑动和ACK确认

### 3. 自适应分片大小
- 根据文件大小自动调整分片大小
- 小文件（<1MB）: 1KB分片
- 中等文件（<100MB）: 64KB分片
- 大文件（>=100MB）: 1MB分片

### 4. ACK确认机制
- 每个分片都有ACK确认
- 支持成功和失败ACK
- 超时重试机制

### 5. 校验和验证
- 使用SHA256计算分片校验和
- 接收方验证分片完整性
- 校验失败时请求重传

## 使用示例

```go
// 创建传输管理器
logger := zap.NewNop()
transferMgr := NewTransferManager(logger)

// 发送文件
conn, _ := net.Dial("tcp", "localhost:8080")
err := transferMgr.SendFile(conn, "/path/to/file.txt", "user1", 64*1024)

// 接收文件
err = transferMgr.ReceiveFile(conn, "/path/to/save/file.txt")

// 获取传输进度
progress := transferMgr.GetTransferProgress("file_id")
completed, total := progress.GetProgress()
percent := progress.GetPercent()

// 取消传输
err = transferMgr.CancelTransfer("file_id")
```

## 配置参数

### 默认配置
- 默认分片大小: 64KB
- 默认窗口大小: 10
- 默认超时时间: 30秒
- 默认重试次数: 3次
- 自适应速率: 10%

### 可调参数
- 分片大小范围: 1KB - 1MB
- 窗口大小范围: 1 - 100
- 超时时间: 5秒 - 300秒
- 重试次数: 1 - 10次

## 错误处理

- 网络连接错误
- 文件读写错误
- 分片校验失败
- 超时错误
- 断点续传错误

## 性能优化

1. **内存管理**: 使用缓冲区池减少内存分配
2. **并发控制**: 使用读写锁保护共享数据
3. **网络优化**: 设置合适的缓冲区大小
4. **自适应算法**: 根据网络状况调整传输参数

## 扩展性

- 支持自定义协议处理器
- 支持自定义窗口算法
- 支持自定义进度显示
- 支持插件式扩展 