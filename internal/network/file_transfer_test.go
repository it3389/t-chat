package network

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"t-chat/internal/file"
	"t-chat/internal/pinecone/router"
)

// MockPineconeService 模拟PineconeService
type MockPineconeService struct {
	nodeID   string
	messages []Message
}

func NewMockPineconeService(nodeID string) *MockPineconeService {
	return &MockPineconeService{
		nodeID:   nodeID,
		messages: make([]Message, 0),
	}
}

// 实现NetworkServiceInterface接口
func (m *MockPineconeService) GetNodeID() string {
	return m.nodeID
}

func (m *MockPineconeService) SendMessage(peerID string, message *Message) error {
	m.messages = append(m.messages, *message)
	return nil
}

func (m *MockPineconeService) GetLastMessage() *Message {
	if len(m.messages) == 0 {
		return nil
	}
	return &m.messages[len(m.messages)-1]
}

func (m *MockPineconeService) GetMessages() []Message {
	return m.messages
}

// 实现PineconeServiceInterface接口的其他方法
func (m *MockPineconeService) GetPineconeAddr() string {
	return "pinecone://localhost:7777"
}

func (m *MockPineconeService) GetNetworkInfo() map[string]interface{} {
	return map[string]interface{}{"nodeID": m.nodeID}
}

func (m *MockPineconeService) SendMessagePacket(toAddr string, packet *MessagePacket) error {
	// 将MessagePacket转换为Message并存储
	message := &Message{
		Type:      packet.Type,
		From:      packet.From,
		To:        packet.To,
		Content:   packet.Content,
		Timestamp: packet.Timestamp,
		Priority:  MessagePriorityHigh,
	}
	
	// 转换Metadata，将结构体转换为map[string]interface{}
	if packet.Metadata != nil {
		message.Metadata = make(map[string]interface{})
		for key, value := range packet.Metadata {
			if key == "file_request" {
				if reqStruct, ok := value.(*FileRequestMsg); ok {
					// 将FileRequestMsg转换为map[string]interface{}
					message.Metadata[key] = map[string]interface{}{
						"file_id":   reqStruct.FileID,
						"file_name": reqStruct.FileName,
						"size":      float64(reqStruct.Size),
						"total":     float64(reqStruct.Total),
						"checksum":  reqStruct.Checksum,
					}
					// 设置FileID
					message.FileID = reqStruct.FileID
				} else {
					message.Metadata[key] = value
				}
			} else if key == "file_ack" {
				if ackStruct, ok := value.(*FileAckMsg); ok {
					// 将FileAckMsg转换为map[string]interface{}
					message.Metadata[key] = map[string]interface{}{
						"file_id":        ackStruct.FileID,
						"received_index": float64(ackStruct.ReceivedIndex),
					}
					// 设置FileID
					message.FileID = ackStruct.FileID
				} else {
					message.Metadata[key] = value
				}
			} else {
					message.Metadata[key] = value
			}
		}
	}
	
	m.messages = append(m.messages, *message)
	return nil
}

func (m *MockPineconeService) GetAllPeerAddrs() []string {
	return []string{}
}

func (m *MockPineconeService) GetStartTime() time.Time {
	return time.Now()
}

func (m *MockPineconeService) FriendList() FriendListLike {
	return nil
}

func (m *MockPineconeService) GetUsernameByPubKey(pubkey string) (string, bool) {
	return "", false
}

func (m *MockPineconeService) GetPubKeyByUsername(username string) (string, bool) {
	return "", false
}

func (m *MockPineconeService) GetRouteDiscoveryService() RouteDiscoveryServiceInterface {
	return nil
}

func (m *MockPineconeService) GetMDNSService() MDNSServiceInterface {
	return nil
}

func (m *MockPineconeService) GetRouter() *router.Router {
	return nil
}

func (m *MockPineconeService) GetConfig() *NetworkConfig {
	return &NetworkConfig{}
}

func (m *MockPineconeService) GetPublicKeyHex() string {
	return "mock_public_key_hex"
}

// NetworkServiceInterface methods
func (m *MockPineconeService) Start(ctx context.Context) error {
	return nil
}

func (m *MockPineconeService) Stop() error {
	return nil
}

func (m *MockPineconeService) IsRunning() bool {
	return true
}

func (m *MockPineconeService) SetConfig(config *NetworkConfig) error {
	return nil
}

func (m *MockPineconeService) ConnectToPeer(peerAddr string) error {
	return nil
}

func (m *MockPineconeService) TriggerRouteRediscovery(targetAddr string) {
}

func (m *MockPineconeService) GetBufferStats() map[string]interface{} {
	return map[string]interface{}{}
}

func (m *MockPineconeService) GetBufferManagerInfo() map[string]interface{} {
	return map[string]interface{}{}
}

// MockLogger 模拟Logger
type MockLogger struct{}

func (l *MockLogger) Info(msg string, args ...interface{}) {
	fmt.Printf("[INFO] %s\n", fmt.Sprintf(msg, args...))
}

func (l *MockLogger) Infof(format string, args ...interface{}) {
	fmt.Printf("[INFO] %s\n", fmt.Sprintf(format, args...))
}

func (l *MockLogger) Error(msg string, args ...interface{}) {
	fmt.Printf("[ERROR] %s\n", fmt.Sprintf(msg, args...))
}

func (l *MockLogger) Errorf(format string, args ...interface{}) {
	fmt.Printf("[ERROR] %s\n", fmt.Sprintf(format, args...))
}

func (l *MockLogger) Warn(msg string, args ...interface{}) {
	fmt.Printf("[WARN] %s\n", fmt.Sprintf(msg, args...))
}

func (l *MockLogger) Warnf(format string, args ...interface{}) {
	fmt.Printf("[WARN] %s\n", fmt.Sprintf(format, args...))
}

func (l *MockLogger) Debug(msg string, args ...interface{}) {
	// Debug日志已禁用控制台输出
}

func (l *MockLogger) Debugf(format string, args ...interface{}) {
	// Debug日志已禁用控制台输出
}

func (l *MockLogger) Printf(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

// MockConfirmationCallback 模拟用户确认回调
type MockConfirmationCallback struct {
	acceptTransfer bool
	rejectReason   string
}

func (m *MockConfirmationCallback) OnFileTransferRequest(req *FileTransferRequest) (bool, string, error) {
	if m.acceptTransfer {
		return true, "accepted", nil
	}
	return false, m.rejectReason, nil
}

func (m *MockConfirmationCallback) OnTransferProgress(fileID string, progress float64) {
	fmt.Printf("Transfer progress: %s - %.2f%%\n", fileID, progress*100)
}

func (m *MockConfirmationCallback) OnTransferComplete(fileID string, success bool, err error) {
	fmt.Printf("Transfer complete: %s - success: %t, error: %v\n", fileID, success, err)
}

func (m *MockConfirmationCallback) OnTransferCanceled(fileID string, reason string) {
	fmt.Printf("Transfer cancelled: %s - reason: %s\n", fileID, reason)
}

// TestFileTransferConfirmationMechanism 测试文件传输确认机制
func TestFileTransferConfirmationMechanism(t *testing.T) {
	// 创建临时目录
	tempDir := t.TempDir()
	
	// 创建测试文件
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := "Hello, this is a test file for transfer confirmation mechanism."
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// 创建配置
	config := &FileTransferConfig{
		DataDir:             tempDir,
		TempDir:             tempDir,
		ChunkSize:           1024,
		MaxConcurrent:       5,
		Timeout:             30 * time.Second,
		RetryCount:          3,
		MaxFileSize:         10 * 1024 * 1024, // 10MB
		EnableEncryption:    false,
		RetryDelay:          1 * time.Second,
		MaxRetryDelay:       10 * time.Second,
		BackoffFactor:       2.0,
		ConfirmationTimeout: 30 * time.Second,
	}
	
	// 创建模拟服务
	senderService := NewMockPineconeService("sender-node")
	receiverService := NewMockPineconeService("receiver-node")
	logger := &MockLogger{}
	
	// 创建文件传输服务
	senderFTS := NewFileTransferService(senderService, logger, config)
	receiverFTS := NewFileTransferService(receiverService, logger, config)
	
	// 设置接收方确认回调（接受传输）
	acceptCallback := &MockConfirmationCallback{
		acceptTransfer: true,
	}
	receiverFTS.SetUserConfirmationCallback(acceptCallback)
	
	// 启动服务
	ctx := context.Background()
	err = senderFTS.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start sender service: %v", err)
	}
	
	err = receiverFTS.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start receiver service: %v", err)
	}
	
	// 测试发送文件
	err = senderFTS.SendFile(testFile, "receiver-node")
	if err != nil {
		t.Fatalf("Failed to send file: %v", err)
	}
	
	// 验证文件请求消息
	lastMsg := senderService.GetLastMessage()
	if lastMsg == nil {
		t.Fatal("No message sent")
	}
	
	if lastMsg.Type != MessageTypeFileRequest {
		t.Errorf("Expected MessageTypeFileRequest, got %v", lastMsg.Type)
	}
	
	if lastMsg.To != "receiver-node" {
		t.Errorf("Expected receiver-node, got %s", lastMsg.To)
	}
	
	// 模拟接收方处理文件请求
	err = receiverFTS.HandleFileRequest(lastMsg)
	if err != nil {
		t.Fatalf("Failed to handle file request: %v", err)
	}
	
	// 等待用户确认处理完成
	time.Sleep(100 * time.Millisecond)
	
	// 验证接收方发送了ACK
	receiverMessages := receiverService.GetMessages()
	if len(receiverMessages) == 0 {
		t.Fatal("Receiver should have sent ACK message")
	}
	
	ackMsg := &receiverMessages[len(receiverMessages)-1]
	if ackMsg.Type != MessageTypeFileAck {
		t.Errorf("Expected MessageTypeFileAck, got %v", ackMsg.Type)
	}
	
	// 使用协议适配器解析ACK消息
	protocolAdapter := NewFileProtocolAdapter(&MockLogger{})
	parsedAck, err := protocolAdapter.ExtractFileAckFromMessage(ackMsg)
	if err != nil {
		t.Fatalf("Failed to parse file ACK: %v", err)
	}
	
	if parsedAck.ChunkIndex != -1 {
		t.Errorf("Expected ChunkIndex -1 for file request ACK, got %d", parsedAck.ChunkIndex)
	}
	
	// 模拟发送方接收ACK
	err = senderFTS.HandleFileAck(ackMsg)
	if err != nil {
		t.Fatalf("Failed to handle file ACK: %v", err)
	}
	
	// 验证传输状态
	senderState, exists := senderFTS.GetTransferState(lastMsg.FileID)
	if !exists {
		t.Fatal("Sender transfer state not found")
	}
	
	if senderState.State != TransferStateTransferring && senderState.State != TransferStateAccepted {
		t.Errorf("Expected transfer state to be Transferring or Accepted, got %v", senderState.State)
	}
	
	t.Logf("Test completed successfully. Transfer state: %v", senderState.State)
}

// TestFileChunkHashVerification 测试文件分片哈希校验
func TestFileChunkHashVerification(t *testing.T) {
	// 创建协议适配器
	adapter := NewFileProtocolAdapter(&MockLogger{})
	
	// 创建测试数据
	testData := []byte("Hello, this is test data for hash verification!")
	
	// 计算正确的哈希值
	hash := sha256.Sum256(testData)
	correctHash := hex.EncodeToString(hash[:])
	
	// 创建原始分片
	originalChunk := &file.Chunk{
		Index:  0,
		Offset: 0,
		Size:   len(testData),
		Data:   testData,
		Hash:   hash[:], // 使用原始哈希字节数组
		Done:   false,
	}
	
	// 测试编码过程
	networkChunk := adapter.ConvertToNetworkFileChunk(originalChunk, "test-file-id", "test.txt", 1)
	
	// 验证网络层分片的校验和字段
	if networkChunk.Checksum != correctHash {
		t.Errorf("网络层分片校验和不正确: 期望 %s, 实际 %s", correctHash, networkChunk.Checksum)
	}
	
	// 测试解码过程
	decodedChunk := adapter.ConvertFromNetworkFileChunk(networkChunk)
	
	// 验证解码后的数据
	if !bytes.Equal(decodedChunk.Data, testData) {
		t.Errorf("解码后数据不匹配: 期望 %v, 实际 %v", testData, decodedChunk.Data)
	}
	
	// 验证解码后的哈希值
	decodedHashStr := hex.EncodeToString(decodedChunk.Hash)
	if decodedHashStr != correctHash {
		t.Errorf("解码后哈希值不匹配: 期望 %s, 实际 %s", correctHash, decodedHashStr)
	}
	
	// 重新计算解码后数据的哈希值进行验证
	newHash := sha256.Sum256(decodedChunk.Data)
	newHashStr := hex.EncodeToString(newHash[:])
	if newHashStr != correctHash {
		t.Errorf("重新计算的哈希值不匹配: 期望 %s, 实际 %s", correctHash, newHashStr)
	}
	
	t.Logf("分片哈希校验测试通过: Hash=%s", correctHash)
}

// TestFileChunkHashVerificationWithCorruption 测试损坏数据的哈希校验
func TestFileChunkHashVerificationWithCorruption(t *testing.T) {
	adapter := NewFileProtocolAdapter(&MockLogger{})
	
	// 创建测试数据
	testData := []byte("Original data for corruption test")
	hash := sha256.Sum256(testData)
	correctHash := hex.EncodeToString(hash[:])
	
	// 创建网络分片消息
	networkChunk := &FileChunkMsg{
		FileID:   "test-file-id",
		Index:    0,
		Total:    1,
		Data:     base64.StdEncoding.EncodeToString(testData),
		Checksum: correctHash,
		FileName: "test.txt",
	}
	
	// 模拟数据传输过程中的损坏
	corruptedData := []byte("Corrupted data for corruption test")
	corruptedBase64 := base64.StdEncoding.EncodeToString(corruptedData)
	networkChunk.Data = corruptedBase64
	
	// 解码损坏的分片
	decodedChunk := adapter.ConvertFromNetworkFileChunk(networkChunk)
	
	// 验证解码后的数据确实是损坏的
	if bytes.Equal(decodedChunk.Data, testData) {
		t.Error("损坏的数据不应该与原始数据相同")
	}
	
	// 重新计算哈希值
	newHash := sha256.Sum256(decodedChunk.Data)
	newHashStr := hex.EncodeToString(newHash[:])
	
	// 验证哈希值不匹配（这是预期的）
	if newHashStr == correctHash {
		t.Error("损坏数据的哈希值不应该与原始哈希值匹配")
	}
	
	t.Logf("损坏数据哈希校验测试通过: 原始Hash=%s, 损坏Hash=%s", correctHash, newHashStr)
}

// TestFileTransferRejection 测试文件传输拒绝机制
func TestFileTransferRejection(t *testing.T) {
	// 创建临时目录
	tempDir := t.TempDir()
	
	// 创建测试文件
	testFile := filepath.Join(tempDir, "test.txt")
	testContent := "Test file for rejection"
	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// 创建配置
	config := DefaultFileTransferConfig()
	config.DataDir = tempDir
	config.TempDir = tempDir
	
	// 创建模拟服务
	senderService := NewMockPineconeService("sender-node")
	receiverService := NewMockPineconeService("receiver-node")
	logger := &MockLogger{}
	
	// 创建文件传输服务
	senderFTS := NewFileTransferService(senderService, logger, config)
	receiverFTS := NewFileTransferService(receiverService, logger, config)
	
	// 设置接收方确认回调（拒绝传输）
	rejectCallback := &MockConfirmationCallback{
		acceptTransfer: false,
		rejectReason:   "User declined the transfer",
	}
	receiverFTS.SetUserConfirmationCallback(rejectCallback)
	
	// 启动服务
	ctx := context.Background()
	senderFTS.Start(ctx)
	receiverFTS.Start(ctx)
	
	// 测试发送文件
	err = senderFTS.SendFile(testFile, "receiver-node")
	if err != nil {
		t.Fatalf("Failed to send file: %v", err)
	}
	
	// 获取文件请求消息
	lastMsg := senderService.GetLastMessage()
	
	// 模拟接收方处理文件请求
	err = receiverFTS.HandleFileRequest(lastMsg)
	if err != nil {
		t.Fatalf("Failed to handle file request: %v", err)
	}
	
	// 等待用户确认处理完成
	time.Sleep(100 * time.Millisecond)
	
	// 验证接收方发送了NACK
	receiverMessages := receiverService.GetMessages()
	if len(receiverMessages) == 0 {
		t.Fatal("Receiver should have sent NACK message")
	}
	
	nackMsg := &receiverMessages[len(receiverMessages)-1]
	if nackMsg.Type != MessageTypeFileNack {
		t.Errorf("Expected MessageTypeFileNack, got %v", nackMsg.Type)
	}
	
	// 模拟发送方接收NACK
	err = senderFTS.HandleFileNack(nackMsg)
	if err != nil {
		t.Fatalf("Failed to handle file NACK: %v", err)
	}
	
	// 验证传输状态
	senderState, exists := senderFTS.GetTransferState(lastMsg.FileID)
	if !exists {
		t.Fatal("Sender transfer state not found")
	}
	
	if senderState.State != TransferStateRejected {
		t.Errorf("Expected transfer state to be Rejected, got %v", senderState.State)
	}
	
	if senderState.ErrorMessage != "User declined the transfer" {
		t.Errorf("Expected error message 'User declined the transfer', got '%s'", senderState.ErrorMessage)
	}
	
	t.Logf("Rejection test completed successfully. Transfer state: %v, Error: %s", senderState.State, senderState.ErrorMessage)
}