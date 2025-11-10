// Package cmd 提供t-chat应用程序的命令行界面
//
// 文件传输命令模块负责处理文件相关的所有操作，包括：
// - 文件发送和接收
// - 文件分片传输管理
// - 传输进度监控
// - 断点续传功能
// - 任务管理和恢复
package cmd

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"t-chat/internal/file"
	"t-chat/internal/network"
	"t-chat/internal/util"
	"github.com/spf13/cobra"
)

var fileSendTo string
var fileSendPath string
var fileRecvFrom string
var fileRecvSave string

// generateFileMessageID 生成唯一的文件消息ID
//
// 返回值：
//   string - 32字符的十六进制字符串作为文件消息唯一标识符
//
// 处理逻辑：
//   1. 生成16字节的随机数据
//   2. 转换为十六进制字符串
//   3. 返回作为文件消息ID
//
// 安全性：
//   使用crypto/rand确保随机性，避免ID冲突
func generateFileMessageID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// maxConcurrentChunks 并发窗口大小
//
// 定义同时传输的文件分片数量上限，用于：
// - 控制网络带宽使用
// - 避免过多并发连接
// - 提高传输稳定性
// 针对Pinecone网络特性优化：单线程发送
const maxConcurrentChunks = 1

// fileCmd 文件传输主命令
//
// 提供文件传输相关的所有子命令，包括：
// - send: 发送文件
// - recv: 接收文件
// - progress: 查看传输进度
// - tasks: 列出传输任务
// - resume: 恢复发送任务
// - cancel: 取消传输任务
// - recv-resume: 恢复接收任务
var fileCmd = &cobra.Command{
	Use:   "file",
	Short: "文件传输相关命令",
	Long:  "文件传输功能模块，提供文件发送、接收、进度监控和任务管理功能",
}

// sendFileCmd 发送文件命令
//
// 功能特性：
// - 支持大文件分片传输
// - 自动生成文件校验和
// - 支持断点续传
// - 实时进度显示
// - 错误重试机制
var sendFileCmd = &cobra.Command{
	Use:   "send",
	Short: "发送文件给好友",
	Long:  "通过 Pinecone 网络将文件分片发送给好友，支持 --to 和 --file 参数，支持断点续传。",
	Run: func(cmd *cobra.Command, args []string) {
		to := fileSendTo
		filePath := fileSendPath
		reader := bufio.NewReader(os.Stdin)
		if to == "" {
			fmt.Print("请输入好友用户名: ")
			to, _ = reader.ReadString('\n')
			to = strings.TrimSpace(to)
		}
		if filePath == "" {
			fmt.Print("请输入要发送的文件路径: ")
			filePath, _ = reader.ReadString('\n')
			filePath = strings.TrimSpace(filePath)
		}
		// 检查文件是否存在
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Printf("❌ 文件不存在: %s\n", filePath)
			return
		}
		
		// 读取文件内容
		fileData, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("❌ 读取文件失败: %v\n", err)
			return
		}
		
		// 获取目标地址（支持好友用户名或直接使用公钥地址）
		var pineconeAddr string
		
		// 检查输入是否为64字符的十六进制公钥地址
		if len(to) == 64 {
			// 验证是否为有效的十六进制字符串
			if _, err := hex.DecodeString(to); err == nil {
				pineconeAddr = to
				fmt.Printf("📤 使用公钥地址发送文件: %s\n", to[:16]+"..."+to[48:])
			} else {
				fmt.Printf("❌ 无效的公钥地址格式: %s\n", to)
				fmt.Println("💡 提示: 公钥地址必须是64位十六进制字符串")
				return
			}
		} else {
			// 尝试从好友列表获取地址
			addr, err := friendList.GetPineconeAddr(to)
			if err != nil {
				fmt.Printf("❌ 好友 '%s' 不存在: %v\n", to, err)
				fmt.Println("💡 提示: 请先使用 'friend add' 命令添加好友，或直接使用64位十六进制公钥地址")
				return
			}
			
			// 验证获取到的地址格式
			if len(addr) != 64 {
				fmt.Printf("❌ 好友 '%s' 的 Pinecone 地址格式无效 (长度: %d，期望: 64): %s\n", to, len(addr), addr)
				fmt.Println("💡 提示: 请检查好友信息是否正确，或重新添加好友")
				return
			}
			pineconeAddr = addr
			fmt.Printf("📤 发送文件给好友 '%s'\n", to)
		}

		fileName := filepath.Base(filePath)
		checksum := util.CalculateChecksum(fileData)
		fileID := generateFileMessageID()
		chunkSize := 30 * 1024 // 30KB - 严格符合Pinecone网络32KB消息限制
		totalChunks := (len(fileData) + chunkSize - 1) / chunkSize

		// 1. 发送文件传输请求
		req := network.FileRequestMsg{
			FileID:   fileID,
			FileName: fileName,
			Size:     int64(len(fileData)),
			Total:    totalChunks,
			Checksum: checksum,
		}
		packetReq := network.MessagePacket{
			From:    "me",
			To:      to,
			Type:    network.MessageTypeFileRequest,
			Content: fileName,
			Data:    nil,
			Metadata: map[string]interface{}{
				"file_request": req,
			},
		}
		if pineconeService != nil {
			err := pineconeService.SendMessagePacket(pineconeAddr, &packetReq)
			if err != nil {
				fmt.Printf("❌ 发送文件请求失败: %v\n", err)
				return
			}
		} else {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}

		ackCh := make(chan int, 1)
		// 注册 Pinecone 消息监听协程，监听 file_ack
		stopAck := make(chan struct{})
		go func() {
			for {
				select {
				case <-stopAck:
					return
				case msg := <-pineconeService.GetMessageChannel():
					if msg.Type == network.MessageTypeFileAck {
						if v, ok := msg.Metadata["file_ack"]; ok {
							if m, ok := v.(map[string]interface{}); ok {
								fileID2 := m["file_id"].(string)
								if fileID2 == fileID {
									idx := int(m["received_index"].(float64))
									ackCh <- idx
								}
							}
						}
					}
				}
			}
		}()

		// 2. 逐片发送文件分片，等待 ack
		// 在 file send 命令实现断点恢复
		// 检查是否有未完成的发送任务
		sess := util.LoadSendSession(fileID)
		currentIdx := 0
		if sess != nil && sess.FileID == fileID && sess.FileName == fileName && sess.Checksum == checksum {
			currentIdx = sess.CurrentIdx
			fmt.Printf("🔄 检测到未完成的发送任务，自动从分片 %d 恢复\n", currentIdx+1)
		}
		inflight := 0
		pending := make(map[int]struct{})
		ackMap := make(map[int]bool)
		for currentIdx < totalChunks {
			// 动态进度条
			done := 0
			for i := 0; i < totalChunks; i++ { if ackMap[i] { done++ } }
			barLen := 30
			bar := strings.Repeat("\033[32m#\033[0m", done*barLen/totalChunks) + strings.Repeat("-", barLen-done*barLen/totalChunks)
			fmt.Printf("\r[%-30s] %d/%d (\033[36m%.2f%%\033[0m)", bar, done, totalChunks, float64(done)*100/float64(totalChunks))
			os.Stdout.Sync()
			// 并发窗口控制
			for inflight < maxConcurrentChunks && currentIdx < totalChunks {
				if _, ok := pending[currentIdx]; ok { currentIdx++; continue }
				start := currentIdx * chunkSize
				end := start + chunkSize
				if end > len(fileData) { end = len(fileData) }
				chunkData := fileData[start:end]
				chunkChecksum := util.CalculateChecksum(chunkData)
				chunkMsg := network.FileChunkMsg{
					FileID:   fileID,
					Index:    currentIdx,
					Total:    totalChunks,
					Data:     base64.StdEncoding.EncodeToString(chunkData),
					Checksum: chunkChecksum,
				}
				if currentIdx == 0 { chunkMsg.FileName = fileName }
				packetChunk := network.MessagePacket{
					From:    "me",
					To:      to,
					Type:    network.MessageTypeFileChunk,
					Content: fileName,
					Data:    chunkData,
					Metadata: map[string]interface{}{"file_chunk": chunkMsg},
				}
				pending[currentIdx] = struct{}{}
				inflight++
				idx := currentIdx
				go func(idx int, pkt network.MessagePacket) {
					pineconeService.SendMessagePacket(pineconeAddr, &pkt)
				}(idx, packetChunk)
				currentIdx++
			}
			// 等待 ack
			timeout := time.After(5 * time.Second)
			select {
			case ackIdx := <-ackCh:
				if _, ok := pending[ackIdx]; ok {
					delete(pending, ackIdx)
					ackMap[ackIdx] = true
					inflight--
				}
			case <-timeout:
				// 超时重发所有未确认分片
				for idx := range pending {
					start := idx * chunkSize
					end := start + chunkSize
					if end > len(fileData) { end = len(fileData) }
					chunkData := fileData[start:end]
					chunkChecksum := util.CalculateChecksum(chunkData)
					chunkMsg := network.FileChunkMsg{
						FileID:   fileID,
						Index:    idx,
						Total:    totalChunks,
						Data:     base64.StdEncoding.EncodeToString(chunkData),
						Checksum: chunkChecksum,
					}
					if idx == 0 { chunkMsg.FileName = fileName }
					packetChunk := network.MessagePacket{
						From:    "me",
						To:      to,
						Type:    network.MessageTypeFileChunk,
						Content: fileName,
						Data:    chunkData,
						Metadata: map[string]interface{}{"file_chunk": chunkMsg},
					}
					go pineconeService.SendMessagePacket(pineconeAddr, &packetChunk)
				}
			}
		}
		fmt.Println()

		// 3. 发送文件完成通知
		completeMsg := network.FileCompleteMsg{FileID: fileID}
		packetComplete := network.MessagePacket{
			From:    "me",
			To:      to,
			Type:    network.MessageTypeFileComplete,
			Content: fileName,
			Metadata: map[string]interface{}{
				"file_complete": completeMsg,
			},
		}
		if pineconeService != nil {
			err := pineconeService.SendMessagePacket(pineconeAddr, &packetComplete)
			if err != nil {
				fmt.Printf("❌ 发送完成通知失败: %v\n", err)
				return
			}
		} else {
			fmt.Println("❌ Pinecone 服务未初始化")
			return
		}
		fmt.Printf("✅ 已通过 Pinecone 向 %s 发送文件: %s (%d bytes, %d 分片)\n", to, fileName, len(fileData), totalChunks)
		// 发送完成后，删除进度文件
		filePath = filepath.Join("files", ".send_"+fileID+".json")
		_ = os.Remove(filePath)
	},
}

// recvFileCmd 接收文件
var recvFileCmd = &cobra.Command{
	Use:   "recv",
	Short: "接收来自好友的文件",
	Long:  "监听本地端口接收文件分片，支持 --from 和 --save 参数，支持断点续传。",
	Run: func(cmd *cobra.Command, args []string) {
		from := fileRecvFrom
		savePath := fileRecvSave
		reader := bufio.NewReader(os.Stdin)
		if from == "" {
			fmt.Print("请输入好友用户名: ")
			from, _ = reader.ReadString('\n')
			from = strings.TrimSpace(from)
		}
		if savePath == "" {
			fmt.Print("请输入保存文件的路径: ")
			savePath, _ = reader.ReadString('\n')
			savePath = strings.TrimSpace(savePath)
		}
		// 检查保存路径
		if savePath == "" {
			savePath = "files/received_file"
		}
		
		// 确保保存目录存在
		saveDir := filepath.Dir(savePath)
		if err := os.MkdirAll(saveDir, 0755); err != nil {
			fmt.Printf("❌ 创建保存目录失败: %v\n", err)
			return
		}
		
		fmt.Printf("📁 文件将保存到: %s\n", savePath)
		fmt.Println("⏳ 等待文件传输...")
		
		// 启动监听
		ln, err := net.Listen("tcp", ":23456")
		if err != nil {
			fmt.Printf("❌ 文件接收监听端口启动失败: %v\n", err)
			return
		}
		defer ln.Close()
		
		fmt.Println("🔗 监听端口 23456，等待连接...")
		conn, err := ln.Accept()
		if err != nil {
			fmt.Printf("❌ 接收连接失败: %v\n", err)
			return
		}
		defer conn.Close()
		
		fmt.Println("✅ 连接已建立，开始接收文件...")
		
		// 创建文件接收器
		receiver := file.NewFileReceiver(savePath, 0) // 文件大小可后续协商
		bufReader := bufio.NewReader(conn)
		
		chunkCount := 0
		for {
			line, err := bufReader.ReadString('\n')
			if err != nil {
				if err.Error() == "EOF" {
					break
				}
				fmt.Printf("❌ 读取数据失败: %v\n", err)
				break
			}
			
			parts := strings.SplitN(strings.TrimSpace(line), "|", 3)
			if len(parts) != 3 {
				fmt.Printf("⚠️ 跳过无效数据行: %s\n", line)
				continue
			}
			
			idx := 0
			sz := 0
			fmt.Sscanf(parts[0], "%d", &idx)
			fmt.Sscanf(parts[1], "%d", &sz)
			
			data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[2]))
			if err != nil {
				fmt.Printf("❌ 解码数据失败: %v\n", err)
				continue
			}
			
			chunk := &file.Chunk{Index: idx, Size: sz, Data: data, Done: true}
			if err := receiver.ReceiveChunk(chunk); err != nil {
				fmt.Printf("❌ 接收分片失败: %v\n", err)
				continue
			}
			
			chunkCount++
			fmt.Printf("📦 已接收分片 %d/%d (大小: %d bytes)\n", idx+1, sz, len(data))
		}
		
		if err := receiver.SaveToFile(); err != nil {
			fmt.Printf("❌ 保存文件失败: %v\n", err)
			return
		}
		
		fmt.Printf("\033[32m✅ 文件接收完成！共接收 %d 个分片\033[0m\n", chunkCount)
	},
}

// progressCmd 查看文件传输进度
var progressCmd = &cobra.Command{
	Use:   "progress",
	Short: "查看当前文件传输进度",
	Run: func(cmd *cobra.Command, args []string) {
		// 这里可以集成文件传输管理器来获取实际进度
		fmt.Println("📊 文件传输进度:")
		fmt.Println("  - 当前无活跃的文件传输任务")
		fmt.Println("  - 历史传输记录:")
		
		// 显示最近的文件传输记录
		fileManager := file.NewManager()
		files := fileManager.GetAllFiles()
		
		if len(files) == 0 {
			fmt.Println("    - 暂无传输记录")
		} else {
			// 显示最近5个文件
			count := 0
			for i := len(files) - 1; i >= 0 && count < 5; i-- {
				f := files[i]
				fmt.Printf("    - %s (%d bytes) - %s\n", 
					f.Name, f.Size, f.ReceivedAt.Format("2006-01-02 15:04:05"))
				count++
			}
		}
	},
}

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "列出所有未完成的文件发送/接收任务",
	Run: func(cmd *cobra.Command, args []string) {
		filesDir := "files"
		files, _ := os.ReadDir(filesDir)
		fmt.Println("📋 未完成的文件任务：")
		found := false
		for _, f := range files {
			name := f.Name()
			if strings.HasPrefix(name, ".send_") && strings.HasSuffix(name, ".json") {
				data, _ := os.ReadFile(filepath.Join(filesDir, name))
				sess := &sendSession{}
				if err := json.Unmarshal(data, sess); err == nil {
					fmt.Printf("[发送] 文件: %s, 进度: %d/%d, 文件ID: %s\n", sess.FileName, sess.CurrentIdx, sess.TotalChunks, sess.FileID)
					found = true
				}
			}
			if strings.HasPrefix(name, ".recv_") && strings.HasSuffix(name, ".json") {
				data, _ := os.ReadFile(filepath.Join(filesDir, name))
				tmp := struct {
					FileName string
					Total    int
					Chunks   [][]byte
					Checksum string
					LastUpdate int64
				}{}
				if err := json.Unmarshal(data, &tmp); err == nil {
					recvCount := 0
					for _, c := range tmp.Chunks { if len(c) > 0 { recvCount++ } }
					fmt.Printf("[接收] 文件: %s, 进度: %d/%d, 文件ID: %s\n", tmp.FileName, recvCount, tmp.Total, strings.TrimSuffix(strings.TrimPrefix(name, ".recv_"), ".json"))
					found = true
				}
			}
		}
		if !found {
			fmt.Println("  - 暂无未完成任务")
		}
	},
}

var resumeCmd = &cobra.Command{
	Use:   "resume <fileid>",
	Short: "恢复指定未完成的文件发送任务",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fileid := args[0]
		sess := util.LoadSendSession(fileid)
		if sess == nil {
			fmt.Printf("❌ 未找到 fileid=%s 的发送任务\n", fileid)
			return
		}
		filePath := filepath.Join("files", sess.FileName)
		fileData, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("❌ 找不到原始文件: %s\n", filePath)
			return
		}
		fmt.Printf("🔄 恢复发送任务: %s (fileid=%s)\n", sess.FileName, fileid)
		// 复用 send 逻辑
		to := fileSendTo
		if to == "" {
			fmt.Print("请输入接收方用户名: ")
			reader := bufio.NewReader(os.Stdin)
			to, _ = reader.ReadString('\n')
			to = strings.TrimSpace(to)
		}
		pineconeAddr, err := friendList.GetPineconeAddr(to)
		if err != nil {
			fmt.Printf("❌ 未找到好友 Pinecone 地址: %v\n", err)
			return
		}
		fileName := sess.FileName
		//checksum := sess.Checksum
		fileID := sess.FileID
		chunkSize := 64 * 1024
		totalChunks := sess.TotalChunks
		currentIdx := sess.CurrentIdx
		ackCh := make(chan int, 1)
		stopAck := make(chan struct{})
		go func() {
			for {
				select {
				case <-stopAck:
					return
				case msg := <-pineconeService.GetMessageChannel():
					if msg.Type == network.MessageTypeFileAck {
						if v, ok := msg.Metadata["file_ack"]; ok {
							if m, ok := v.(map[string]interface{}); ok {
								fileID2 := m["file_id"].(string)
								if fileID2 == fileID {
									idx := int(m["received_index"].(float64))
									ackCh <- idx
								}
							}
						}
					}
				}
			}
		}()
		inflight := 0
		pending := make(map[int]struct{})
		ackMap := make(map[int]bool)
		for currentIdx < totalChunks {
			// 动态进度条
			done := 0
			for i := 0; i < totalChunks; i++ { if ackMap[i] { done++ } }
			barLen := 30
			bar := strings.Repeat("#", done*barLen/totalChunks) + strings.Repeat("-", barLen-done*barLen/totalChunks)
			fmt.Printf("\r[%-30s] %d/%d (%.2f%%)", bar, done, totalChunks, float64(done)*100/float64(totalChunks))
			os.Stdout.Sync()
			// 并发窗口控制
			for inflight < maxConcurrentChunks && currentIdx < totalChunks {
				if _, ok := pending[currentIdx]; ok { currentIdx++; continue }
				start := currentIdx * chunkSize
				end := start + chunkSize
				if end > len(fileData) { end = len(fileData) }
				chunkData := fileData[start:end]
				chunkChecksum := util.CalculateChecksum(chunkData)
				chunkMsg := network.FileChunkMsg{
					FileID:   fileID,
					Index:    currentIdx,
					Total:    totalChunks,
					Data:     base64.StdEncoding.EncodeToString(chunkData),
					Checksum: chunkChecksum,
				}
				if currentIdx == 0 { chunkMsg.FileName = fileName }
				packetChunk := network.MessagePacket{
					From:    "me",
					To:      to,
					Type:    network.MessageTypeFileChunk,
					Content: fileName,
					Data:    chunkData,
					Metadata: map[string]interface{}{"file_chunk": chunkMsg},
				}
				pending[currentIdx] = struct{}{}
				inflight++
				idx := currentIdx
				go func(idx int, pkt network.MessagePacket) {
					pineconeService.SendMessagePacket(pineconeAddr, &pkt)
				}(idx, packetChunk)
				currentIdx++
			}
			// 等待 ack
			timeout := time.After(5 * time.Second)
			select {
			case ackIdx := <-ackCh:
				if _, ok := pending[ackIdx]; ok {
					delete(pending, ackIdx)
					ackMap[ackIdx] = true
					inflight--
				}
			case <-timeout:
				// 超时重发所有未确认分片
				for idx := range pending {
					start := idx * chunkSize
					end := start + chunkSize
					if end > len(fileData) { end = len(fileData) }
					chunkData := fileData[start:end]
					chunkChecksum := util.CalculateChecksum(chunkData)
					chunkMsg := network.FileChunkMsg{
						FileID:   fileID,
						Index:    idx,
						Total:    totalChunks,
						Data:     base64.StdEncoding.EncodeToString(chunkData),
						Checksum: chunkChecksum,
					}
					if idx == 0 { chunkMsg.FileName = fileName }
					packetChunk := network.MessagePacket{
						From:    "me",
						To:      to,
						Type:    network.MessageTypeFileChunk,
						Content: fileName,
						Data:    chunkData,
						Metadata: map[string]interface{}{"file_chunk": chunkMsg},
					}
					go pineconeService.SendMessagePacket(pineconeAddr, &packetChunk)
				}
			}
		}
		fmt.Println()
		close(stopAck)
		fmt.Printf("✅ 已恢复并完成发送: %s (%d bytes, %d 分片)\n", fileName, len(fileData), totalChunks)
		filePath = filepath.Join("files", ".send_"+fileID+".json")
		_ = os.Remove(filePath)
	},
}

var cancelCmd = &cobra.Command{
	Use:   "cancel <fileid>",
	Short: "取消并删除指定任务进度文件",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fileid := args[0]
		cnt := 0
		for _, prefix := range []string{".send_", ".recv_"} {
			filePath := filepath.Join("files", prefix+fileid+".json")
			if _, err := os.Stat(filePath); err == nil {
				_ = os.Remove(filePath)
				fmt.Printf("🗑️ 已删除任务进度文件: %s\n", filePath)
				cnt++
			}
		}
		if cnt == 0 {
			fmt.Printf("❌ 未找到 fileid=%s 的任务进度文件\n", fileid)
		}
	},
}

var recvResumeCmd = &cobra.Command{
	Use:   "recv-resume <fileid>",
	Short: "恢复指定未完成的文件接收任务（等待缺失分片）",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		fileid := args[0]
		filePath := filepath.Join("files", ".recv_"+fileid+".json")
		data, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Printf("❌ 未找到 fileid=%s 的接收任务\n", fileid)
			return
		}
		tmp := struct {
			FileName string
			Total    int
			Chunks   [][]byte
			Checksum string
			LastUpdate int64
		}{}
		if err := json.Unmarshal(data, &tmp); err != nil {
			fmt.Printf("❌ 解析任务进度失败: %v\n", err)
			return
		}
		recvCount := 0
		for _, c := range tmp.Chunks { if len(c) > 0 { recvCount++ } }
		fmt.Printf("🔄 恢复接收任务: %s (fileid=%s), 当前进度: %d/%d\n", tmp.FileName, fileid, recvCount, tmp.Total)
		fmt.Println("请保持程序运行，等待发送端补发缺失分片...")
		// 实际上，接收端 resume 只需保持在线，分片到达时自动合并
	},
}

type sendSession struct {
	FileID      string
	FileName    string
	TotalChunks int
	CurrentIdx  int
	Checksum    string
}

func init() {

	RootCmd.AddCommand(fileCmd)
	fileCmd.AddCommand(sendFileCmd)
	fileCmd.AddCommand(recvFileCmd)
	fileCmd.AddCommand(progressCmd)
	fileCmd.AddCommand(tasksCmd)
	fileCmd.AddCommand(resumeCmd)
	fileCmd.AddCommand(cancelCmd)
	fileCmd.AddCommand(recvResumeCmd)
	sendFileCmd.Flags().StringVarP(&fileSendTo, "to", "t", "", "接收方用户名")
	sendFileCmd.Flags().StringVarP(&fileSendPath, "file", "f", "", "要发送的文件路径")
	recvFileCmd.Flags().StringVarP(&fileRecvFrom, "from", "r", "", "发送方用户名")
	recvFileCmd.Flags().StringVarP(&fileRecvSave, "save", "s", "", "保存文件的路径")
}
