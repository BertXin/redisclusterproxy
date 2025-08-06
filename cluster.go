package main

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ClusterManager Redis集群管理器
type ClusterManager struct {
	nodes     map[string]*ClusterNode // 节点映射
	slots     [16384]string           // slot到节点的映射
	mutex     sync.RWMutex
	config    *Config
	lastUpdate time.Time
}

// ClusterNode Redis集群节点信息
type ClusterNode struct {
	ID       string
	Address  string
	IsMaster bool
	Slots    []SlotRange
	Flags    []string
	Master   string // 如果是slave，指向master的ID
	Health   bool
	LastPing time.Time
}

// SlotRange slot范围
type SlotRange struct {
	Start int
	End   int
}

// NewClusterManager 创建集群管理器
func NewClusterManager(config *Config) *ClusterManager {
	return &ClusterManager{
		nodes:  make(map[string]*ClusterNode),
		config: config,
	}
}

// RefreshClusterInfo 刷新集群信息
func (cm *ClusterManager) RefreshClusterInfo() error {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	LogDebug("正在刷新Redis集群信息...")

	// 尝试从任意一个节点获取集群信息
	for _, nodeAddr := range cm.config.RedisNodes {
		if err := cm.fetchClusterInfoFromNode(nodeAddr); err == nil {
			cm.lastUpdate = time.Now()
			LogInfo("成功从节点 %s 获取集群信息", nodeAddr)
			return nil
		} else {
			LogWarn("从节点 %s 获取集群信息失败: %v", nodeAddr, err)
		}
	}

	return fmt.Errorf("无法从任何节点获取集群信息")
}

// fetchClusterInfoFromNode 从指定节点获取集群信息
func (cm *ClusterManager) fetchClusterInfoFromNode(nodeAddr string) error {
	conn, err := net.DialTimeout("tcp", nodeAddr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("连接节点失败: %v", err)
	}
	defer conn.Close()

	// 发送CLUSTER NODES命令
	_, err = conn.Write([]byte("CLUSTER NODES\r\n"))
	if err != nil {
		return fmt.Errorf("发送命令失败: %v", err)
	}

	// 读取响应
	reader := bufio.NewReader(conn)
	response, err := cm.readClusterNodesResponse(reader)
	if err != nil {
		return fmt.Errorf("读取响应失败: %v", err)
	}

	// 解析集群节点信息
	return cm.parseClusterNodes(response)
}

// readClusterNodesResponse 读取CLUSTER NODES响应
func (cm *ClusterManager) readClusterNodesResponse(reader *bufio.Reader) (string, error) {
	// 读取第一行（响应头）
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(line, "$") {
		return "", fmt.Errorf("无效的响应格式: %s", line)
	}

	// 解析长度
	lengthStr := strings.TrimSpace(line[1:])
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", fmt.Errorf("无效的长度: %s", lengthStr)
	}

	// 读取数据
	data := make([]byte, length)
	_, err = reader.Read(data)
	if err != nil {
		return "", err
	}

	// 读取结尾的\r\n
	reader.ReadString('\n')

	return string(data), nil
}

// parseClusterNodes 解析CLUSTER NODES响应
func (cm *ClusterManager) parseClusterNodes(response string) error {
	lines := strings.Split(strings.TrimSpace(response), "\n")
	
	// 清空现有信息
	cm.nodes = make(map[string]*ClusterNode)
	cm.slots = [16384]string{}

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}

		node, err := cm.parseNodeLine(line)
		if err != nil {
			LogWarn("解析节点信息失败: %v, line: %s", err, line)
			continue
		}

		cm.nodes[node.ID] = node

		// 如果是master节点，更新slot映射
		if node.IsMaster {
			for _, slotRange := range node.Slots {
				for slot := slotRange.Start; slot <= slotRange.End; slot++ {
					cm.slots[slot] = node.Address
				}
			}
		}
	}

	LogInfo("解析完成，共 %d 个节点", len(cm.nodes))
	return nil
}

// parseNodeLine 解析单个节点信息行
func (cm *ClusterManager) parseNodeLine(line string) (*ClusterNode, error) {
	parts := strings.Fields(line)
	if len(parts) < 8 {
		return nil, fmt.Errorf("节点信息格式错误")
	}

	// 处理节点地址，去掉集群总线端口
	address := parts[1]
	if atIndex := strings.Index(address, "@"); atIndex != -1 {
		address = address[:atIndex]
	}

	node := &ClusterNode{
		ID:       parts[0],
		Address:  address,
		Flags:    strings.Split(parts[2], ","),
		Master:   parts[3],
		Health:   true,
		LastPing: time.Now(),
	}

	// 判断是否是master
	for _, flag := range node.Flags {
		if flag == "master" {
			node.IsMaster = true
			break
		}
	}

	// 解析slot范围（从第8个字段开始）
	if node.IsMaster && len(parts) > 8 {
		for i := 8; i < len(parts); i++ {
			slotRange, err := cm.parseSlotRange(parts[i])
			if err != nil {
				LogWarn("解析slot范围失败: %v", err)
				continue
			}
			node.Slots = append(node.Slots, slotRange)
		}
	}

	// 直接使用节点地址，不需要映射
	LogDebug("发现集群节点: %s (Master: %v)", node.Address, node.IsMaster)

	return node, nil
}

// parseSlotRange 解析slot范围
func (cm *ClusterManager) parseSlotRange(slotStr string) (SlotRange, error) {
	if strings.Contains(slotStr, "-") {
		// 范围格式: 0-5460
		parts := strings.Split(slotStr, "-")
		if len(parts) != 2 {
			return SlotRange{}, fmt.Errorf("无效的slot范围: %s", slotStr)
		}
		start, err1 := strconv.Atoi(parts[0])
		end, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return SlotRange{}, fmt.Errorf("无效的slot数字: %s", slotStr)
		}
		return SlotRange{Start: start, End: end}, nil
	} else {
		// 单个slot
		slot, err := strconv.Atoi(slotStr)
		if err != nil {
			return SlotRange{}, fmt.Errorf("无效的slot: %s", slotStr)
		}
		return SlotRange{Start: slot, End: slot}, nil
	}
}

// GetNodeForKey 根据key获取对应的节点地址
func (cm *ClusterManager) GetNodeForKey(key string) string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	slot := cm.calculateSlot(key)
	nodeAddr := cm.slots[slot]
	
	if nodeAddr == "" {
		// 如果没有找到对应的节点，返回第一个可用节点
		if len(cm.config.RedisNodes) > 0 {
			return cm.config.RedisNodes[0]
		}
	}

	return nodeAddr
}

// GetNodeForSlot 根据slot获取对应的节点地址
func (cm *ClusterManager) GetNodeForSlot(slot int) string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if slot >= 0 && slot < 16384 {
		return cm.slots[slot]
	}
	return ""
}

// calculateSlot 计算key对应的slot
func (cm *ClusterManager) calculateSlot(key string) int {
	// 检查是否有hash tag
	start := strings.Index(key, "{")
	if start != -1 {
		end := strings.Index(key[start+1:], "}")
		if end != -1 {
			key = key[start+1 : start+1+end]
		}
	}

	// 使用CRC16算法计算slot
	return int(crc16CCITT([]byte(key))) % 16384
}

// crc16CCITT 实现CRC16-CCITT算法
func crc16CCITT(data []byte) uint16 {
	var crc uint16 = 0x0000
	polynomial := uint16(0x1021)

	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ polynomial
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

// GetRandomNode 获取随机节点（用于不需要特定slot的命令）
func (cm *ClusterManager) GetRandomNode() string {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 优先返回master节点
	for _, node := range cm.nodes {
		if node.IsMaster && node.Health {
			return node.Address
		}
	}

	// 如果没有健康的master，返回配置中的第一个节点
	if len(cm.config.RedisNodes) > 0 {
		return cm.config.RedisNodes[0]
	}

	return ""
}

// IsClusterInfoStale 检查集群信息是否过期
func (cm *ClusterManager) IsClusterInfoStale() bool {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	return time.Since(cm.lastUpdate) > 30*time.Second
}

// GetClusterStats 获取集群统计信息
func (cm *ClusterManager) GetClusterStats() map[string]interface{} {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["total_nodes"] = len(cm.nodes)
	stats["last_update"] = cm.lastUpdate
	
	masterCount := 0
	slaveCount := 0
	for _, node := range cm.nodes {
		if node.IsMaster {
			masterCount++
		} else {
			slaveCount++
		}
	}
	
	stats["master_nodes"] = masterCount
	stats["slave_nodes"] = slaveCount
	
	return stats
}