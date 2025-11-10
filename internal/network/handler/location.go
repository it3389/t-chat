package handler

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"t-chat/internal/network"
)

// LocationMessageHandler 位置消息处理器
// 负责处理位置消息的接收、保存和显示
type LocationMessageHandler struct {
	pineconeService network.PineconeServiceLike
	logger         network.Logger
	saveDir        string // 位置数据保存目录
}

// NewLocationMessageHandler 创建位置消息处理器
func NewLocationMessageHandler(p network.PineconeServiceLike, logger network.Logger) *LocationMessageHandler {
	handler := &LocationMessageHandler{
		pineconeService: p,
		logger:         logger,
		saveDir:        "locations", // 默认保存目录
	}
	
	// 确保保存目录存在
	os.MkdirAll(handler.saveDir, 0755)
	
	return handler
}

// HandleMessage 处理位置消息
func (h *LocationMessageHandler) HandleMessage(msg *network.Message) error {

	
	// 解析位置元数据
	locationInfo := h.parseLocationMetadata(msg)
	
	// 保存位置数据
	_, err := h.saveLocation(locationInfo)
	if err != nil {
		// 保存位置数据失败
		return err
	}
	
	
	// 显示位置信息
	h.displayLocationInfo(locationInfo)
	
	// 计算距离（如果有参考位置）
	h.calculateDistance(locationInfo)
	
	return nil
}

// GetMessageType 获取消息类型
func (h *LocationMessageHandler) GetMessageType() string {
	return network.MessageTypeLocation
}

// GetPriority 获取处理优先级
func (h *LocationMessageHandler) GetPriority() int {
	return network.MessagePriorityNormal
}

// parseLocationMetadata 解析位置元数据
func (h *LocationMessageHandler) parseLocationMetadata(msg *network.Message) *LocationInfo {
	info := &LocationInfo{
		From:     msg.From,
		Time:     msg.Timestamp,
		Content:  msg.Content,
	}
	
	// 从元数据中提取信息
	if msg.Metadata != nil {
		if lat, ok := msg.Metadata["latitude"].(float64); ok {
			info.Latitude = lat
		}
		if lng, ok := msg.Metadata["longitude"].(float64); ok {
			info.Longitude = lng
		}
		if address, ok := msg.Metadata["address"].(string); ok {
			info.Address = address
		}
		if accuracy, ok := msg.Metadata["accuracy"].(float64); ok {
			info.Accuracy = accuracy
		}
		if altitude, ok := msg.Metadata["altitude"].(float64); ok {
			info.Altitude = altitude
		}
		if speed, ok := msg.Metadata["speed"].(float64); ok {
			info.Speed = speed
		}
		if heading, ok := msg.Metadata["heading"].(float64); ok {
			info.Heading = heading
		}
		if locationType, ok := msg.Metadata["type"].(string); ok {
			info.LocationType = locationType
		}
	}
	
	// 如果没有坐标信息，尝试从内容中解析
	if info.Latitude == 0 && info.Longitude == 0 {
		h.parseCoordinatesFromContent(msg.Content, info)
	}
	
	// 如果没有地址信息，尝试从内容中解析
	if info.Address == "" {
		info.Address = h.extractAddressFromContent(msg.Content)
	}
	
	// 设置默认值
	if info.LocationType == "" {
		info.LocationType = "unknown"
	}
	if info.Accuracy == 0 {
		info.Accuracy = 10.0 // 默认精度10米
	}
	
	return info
}

// saveLocation 保存位置数据
func (h *LocationMessageHandler) saveLocation(info *LocationInfo) (string, error) {
	// 生成文件名
	timestamp := info.Time.Format("20060102_150405")
	fileName := fmt.Sprintf("location_%s_%s.json", info.From, timestamp)
	
	// 构建完整路径
	filePath := filepath.Join(h.saveDir, fileName)
	
	// 序列化为JSON
	data, err := network.MarshalJSONPooled(info)
	if err != nil {
		return "", fmt.Errorf("序列化位置数据失败: %w", err)
	}
	
	// 写入文件
	err = os.WriteFile(filePath, data, 0644)
	if err != nil {
		return "", fmt.Errorf("写入位置数据失败: %w", err)
	}
	
	return filePath, nil
}

// displayLocationInfo 显示位置信息
func (h *LocationMessageHandler) displayLocationInfo(info *LocationInfo) {
	// 位置信息显示已简化，仅在需要时输出
}

// calculateDistance 计算距离
func (h *LocationMessageHandler) calculateDistance(info *LocationInfo) {
	// 这里可以计算与参考位置的距离
	// 例如：与用户当前位置的距离
	referenceLat := 39.9042 // 北京天安门坐标（示例）
	referenceLng := 116.4074
	
	if info.Latitude != 0 && info.Longitude != 0 {
		_ = h.calculateHaversineDistance(
			referenceLat, referenceLng,
			info.Latitude, info.Longitude,
		)
	}
}

// parseCoordinatesFromContent 从内容中解析坐标
func (h *LocationMessageHandler) parseCoordinatesFromContent(content string, info *LocationInfo) {
	// 尝试解析常见的坐标格式
	patterns := []struct {
		name string
		regex string
		parse func(string) (float64, float64, error)
	}{
		{
			name: "度分秒格式",
			regex: `(\d+)°(\d+)'(\d+\.?\d*)"[NS],\s*(\d+)°(\d+)'(\d+\.?\d*)"[EW]`,
			parse: h.parseDMSFormat,
		},
		{
			name: "十进制格式",
			regex: `(-?\d+\.\d+),\s*(-?\d+\.\d+)`,
			parse: h.parseDecimalFormat,
		},
		{
			name: "Google Maps格式",
			regex: `@(-?\d+\.\d+),(-?\d+\.\d+)`,
			parse: h.parseGoogleMapsFormat,
		},
	}
	
	for _, pattern := range patterns {
		if lat, lng, err := pattern.parse(content); err == nil {
			info.Latitude = lat
			info.Longitude = lng
			break
		}
	}
}

// extractAddressFromContent 从内容中提取地址
func (h *LocationMessageHandler) extractAddressFromContent(content string) string {
	// 简单的地址提取逻辑
	// 在实际应用中，可以使用更复杂的NLP技术
	
	// 移除坐标信息
	content = h.removeCoordinatesFromText(content)
	
	// 清理文本
	content = strings.TrimSpace(content)
	
	// 如果内容看起来像地址，直接返回
	if len(content) > 5 && !strings.Contains(content, "http") {
		return content
	}
	
	return ""
}

// removeCoordinatesFromText 从文本中移除坐标信息
func (h *LocationMessageHandler) removeCoordinatesFromText(text string) string {
	// 移除常见的坐标格式
	patterns := []string{
		`-?\d+\.\d+,\s*-?\d+\.\d+`,           // 十进制坐标
		`\d+°\d+'\d+\.?\d*"[NS],\s*\d+°\d+'\d+\.?\d*"[EW]`, // 度分秒格式
		`@-?\d+\.\d+,-?\d+\.\d+`,             // Google Maps格式
	}
	
	for _, pattern := range patterns {
		text = strings.ReplaceAll(text, pattern, "")
	}
	
	return text
}

// parseDMSFormat 解析度分秒格式
func (h *LocationMessageHandler) parseDMSFormat(text string) (float64, float64, error) {
	// 示例: 39°54'15.12"N, 116°24'26.64"E
	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无效的度分秒格式")
	}
	
	lat, err := h.parseDMS(parts[0])
	if err != nil {
		return 0, 0, err
	}
	
	lng, err := h.parseDMS(parts[1])
	if err != nil {
		return 0, 0, err
	}
	
	return lat, lng, nil
}

// parseDMS 解析单个度分秒值
func (h *LocationMessageHandler) parseDMS(dms string) (float64, error) {
	// 移除空格和引号
	dms = strings.ReplaceAll(dms, " ", "")
	dms = strings.ReplaceAll(dms, "\"", "")
	
	// 检查方向
	var multiplier float64 = 1
	if strings.Contains(dms, "S") || strings.Contains(dms, "W") {
		multiplier = -1
	}
	
	// 移除方向标识
	dms = strings.ReplaceAll(dms, "N", "")
	dms = strings.ReplaceAll(dms, "S", "")
	dms = strings.ReplaceAll(dms, "E", "")
	dms = strings.ReplaceAll(dms, "W", "")
	
	// 解析度分秒
	parts := strings.Split(dms, "°")
	if len(parts) != 2 {
		return 0, fmt.Errorf("无效的度分秒格式")
	}
	
	degrees, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, err
	}
	
	minutesParts := strings.Split(parts[1], "'")
	if len(minutesParts) != 2 {
		return 0, fmt.Errorf("无效的度分秒格式")
	}
	
	minutes, err := strconv.ParseFloat(minutesParts[0], 64)
	if err != nil {
		return 0, err
	}
	
	seconds, err := strconv.ParseFloat(minutesParts[1], 64)
	if err != nil {
		return 0, err
	}
	
	// 转换为十进制
	decimal := degrees + minutes/60 + seconds/3600
	return decimal * multiplier, nil
}

// parseDecimalFormat 解析十进制格式
func (h *LocationMessageHandler) parseDecimalFormat(text string) (float64, float64, error) {
	// 示例: 39.9042, 116.4074
	parts := strings.Split(text, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("无效的十进制格式")
	}
	
	lat, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return 0, 0, err
	}
	
	lng, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return 0, 0, err
	}
	
	return lat, lng, nil
}

// parseGoogleMapsFormat 解析Google Maps格式
func (h *LocationMessageHandler) parseGoogleMapsFormat(text string) (float64, float64, error) {
	// 示例: @39.9042,116.4074
	text = strings.TrimPrefix(text, "@")
	return h.parseDecimalFormat(text)
}

// calculateHaversineDistance 计算两点间的距离（Haversine公式）
func (h *LocationMessageHandler) calculateHaversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371 // 地球半径（公里）
	
	// 转换为弧度
	lat1Rad := lat1 * math.Pi / 180
	lng1Rad := lng1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lng2Rad := lng2 * math.Pi / 180
	
	// 计算差值
	deltaLat := lat2Rad - lat1Rad
	deltaLng := lng2Rad - lng1Rad
	
	// Haversine公式
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) +
		math.Cos(lat1Rad)*math.Cos(lat2Rad)*
			math.Sin(deltaLng/2)*math.Sin(deltaLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	
	return R * c
}

// getDirectionDescription 获取方向描述
func (h *LocationMessageHandler) getDirectionDescription(heading float64) string {
	directions := []string{"北", "东北", "东", "东南", "南", "西南", "西", "西北"}
	index := int((heading + 22.5) / 45) % 8
	return directions[index]
}

// LocationInfo 位置信息结构
type LocationInfo struct {
	From         string    `json:"from"`          // 发送者
	Time         time.Time `json:"time"`          // 时间
	Content      string    `json:"content"`       // 原始内容
	Latitude     float64   `json:"latitude"`      // 纬度
	Longitude    float64   `json:"longitude"`     // 经度
	Address      string    `json:"address"`       // 地址
	Accuracy     float64   `json:"accuracy"`      // 精度（米）
	Altitude     float64   `json:"altitude"`      // 海拔（米）
	Speed        float64   `json:"speed"`         // 速度（km/h）
	Heading      float64   `json:"heading"`       // 方向（度）
	LocationType string    `json:"location_type"` // 位置类型
}

// 工具方法

// GetLocationStats 获取位置统计信息
func (h *LocationMessageHandler) GetLocationStats() map[string]interface{} {
	// 统计保存目录中的位置文件
	files, err := os.ReadDir(h.saveDir)
	if err != nil {
		return map[string]interface{}{
			"error": err.Error(),
		}
	}
	
	stats := map[string]interface{}{
		"total_locations": len(files),
		"save_directory":  h.saveDir,
		"users":          make(map[string]int),
		"types":          make(map[string]int),
	}
	
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			// 解析文件名获取用户信息
			parts := strings.Split(strings.TrimSuffix(file.Name(), ".json"), "_")
			if len(parts) >= 2 {
				user := parts[1]
				stats["users"].(map[string]int)[user]++
			}
		}
	}
	
	return stats
}

// LoadLocationHistory 加载位置历史
func (h *LocationMessageHandler) LoadLocationHistory(user string, limit int) ([]*LocationInfo, error) {
	var locations []*LocationInfo
	
	files, err := os.ReadDir(h.saveDir)
	if err != nil {
		return nil, err
	}
	
	count := 0
	for _, file := range files {
		if count >= limit {
			break
		}
		
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			// 检查是否是目标用户的位置文件
			if user == "" || strings.Contains(file.Name(), "_"+user+"_") {
				filePath := filepath.Join(h.saveDir, file.Name())
				data, err := os.ReadFile(filePath)
				if err != nil {
					continue
				}
				
				var location LocationInfo
				if err := network.UnmarshalJSONPooled(data, &location); err != nil {
					continue
				}
				
				locations = append(locations, &location)
				count++
			}
		}
	}
	
	return locations, nil
}

// GetLocationByTime 根据时间获取位置
func (h *LocationMessageHandler) GetLocationByTime(user string, targetTime time.Time) (*LocationInfo, error) {
	locations, err := h.LoadLocationHistory(user, 100)
	if err != nil {
		return nil, err
	}
	
	var closest *LocationInfo
	var minDiff time.Duration = 24 * time.Hour // 最大时间差
	
	for _, location := range locations {
		diff := abs(location.Time.Sub(targetTime))
		if diff < minDiff {
			minDiff = diff
			closest = location
		}
	}
	
	return closest, nil
}

// abs 计算时间差的绝对值
func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}