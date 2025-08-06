package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// RedisClusterProxy Redis集群代理
type RedisClusterProxy struct {
	config         *Config
	pool           *ConnectionPool
	protocol       *RedisProtocol
	clusterManager *ClusterManager
	listener       net.Listener
	running        bool
	mutex          sync.RWMutex
}

// NewRedisClusterProxy 创建新的Redis集群代理
func NewRedisClusterProxy(config *Config) *RedisClusterProxy {
	return &RedisClusterProxy{
		config:         config,
		pool:           NewConnectionPool(),
		protocol:       &RedisProtocol{},
		clusterManager: NewClusterManager(config),
	}
}

// Start 启动代理服务
func (proxy *RedisClusterProxy) Start() error {
	address := proxy.config.GetProxyAddress()
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("启动代理服务失败: %v", err)
	}

	proxy.listener = listener
	proxy.running = true

	LogInfo("Redis集群代理启动成功，监听地址: %s", address)
	LogInfo("后端Redis节点: %v", proxy.config.RedisNodes)

	// 初始化集群信息
	LogInfo("正在初始化Redis集群信息...")
	if err := proxy.clusterManager.RefreshClusterInfo(); err != nil {
		LogWarn("警告: 初始化集群信息失败: %v", err)
		LogInfo("将使用配置文件中的节点信息")
	} else {
		stats := proxy.clusterManager.GetClusterStats()
		LogInfo("集群信息初始化成功: %v", stats)
	}

	// 启动集群信息定期刷新
	go proxy.startClusterInfoRefresh()

	for proxy.running {
		conn, err := listener.Accept()
		if err != nil {
			if proxy.running {
				LogError("接受连接失败: %v", err)
			}
			continue
		}

		go proxy.handleConnection(conn)
	}

	return nil
}

// startClusterInfoRefresh 启动集群信息定期刷新
func (proxy *RedisClusterProxy) startClusterInfoRefresh() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if proxy.running && proxy.clusterManager.IsClusterInfoStale() {
				LogDebug("刷新集群信息...")
				if err := proxy.clusterManager.RefreshClusterInfo(); err != nil {
					LogWarn("刷新集群信息失败: %v", err)
				}
			}
		default:
			if !proxy.running {
				return
			}
			time.Sleep(1 * time.Second)
		}
	}
}

// Stop 停止代理服务
func (proxy *RedisClusterProxy) Stop() {
	proxy.mutex.Lock()
	defer proxy.mutex.Unlock()

	proxy.running = false
	if proxy.listener != nil {
		proxy.listener.Close()
	}
	proxy.pool.Close()
}

// handleConnection 处理客户端连接
func (proxy *RedisClusterProxy) handleConnection(clientConn net.Conn) {
	defer clientConn.Close()

	clientReader := bufio.NewReader(clientConn)
	LogInfo("新客户端连接: %s", clientConn.RemoteAddr())

	for {
		// 解析客户端命令
		command, err := proxy.protocol.ParseCommand(clientReader)
		if err != nil {
			if err == io.EOF {
				LogInfo("客户端断开连接: %s", clientConn.RemoteAddr())
				return
			}
			LogError("解析命令失败: %v", err)
			proxy.sendError(clientConn, "协议错误")
			return
		}

		if len(command) == 0 {
			continue
		}

		LogDebug("收到命令: %v", command)

		// 处理命令
		err = proxy.handleCommand(clientConn, command)
		if err != nil {
			LogError("处理命令失败: %v", err)
			proxy.sendError(clientConn, err.Error())
		}
	}
}

// handleCommand 处理Redis命令
func (proxy *RedisClusterProxy) handleCommand(clientConn net.Conn, command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("空命令")
	}

	// 选择后端节点（简单轮询，实际应该根据key的hash slot选择）
	backendAddr := proxy.selectBackendNode(command)
	
	// 执行命令并处理重定向
	return proxy.executeCommandWithRedirect(clientConn, command, backendAddr, 0)
}

// executeCommandWithRedirect 执行命令并处理重定向
func (proxy *RedisClusterProxy) executeCommandWithRedirect(clientConn net.Conn, command []string, backendAddr string, redirectCount int) error {
	// 防止无限重定向
	if redirectCount > 5 {
		return fmt.Errorf("重定向次数过多")
	}

	cmdName := ""
	if len(command) > 0 {
		cmdName = strings.ToUpper(command[0])
	}
	
	LogDebug("开始执行命令 %s 到节点 %s", cmdName, backendAddr)

	// 获取后端连接
	backendConn, err := proxy.pool.GetConnection(backendAddr)
	if err != nil {
		return fmt.Errorf("连接后端Redis失败: %v", err)
	}
	defer proxy.pool.ReturnConnection(backendAddr, backendConn)

	LogDebug("成功连接到后端节点 %s，发送命令: %v", backendAddr, command)

	// 发送命令到后端
	err = proxy.sendCommandToBackend(backendConn, command)
	if err != nil {
		return fmt.Errorf("发送命令到后端失败: %v", err)
	}

	LogDebug("命令已发送到节点 %s，开始读取响应...", backendAddr)

	// 读取后端响应
	response, err := proxy.readBackendResponse(backendConn)
	if err != nil {
		LogError("读取后端响应失败: %v", err)
		return fmt.Errorf("读取后端响应失败: %v", err)
	}
	
	// 添加调试日志，对于大响应只显示前面部分
	if len(response) > 500 {
		LogDebug("从节点 %s 收到大响应 (长度: %d): %q...", backendAddr, len(response), response[:500])
	} else {
		LogDebug("从节点 %s 收到完整响应: %q (长度: %d)", backendAddr, response, len(response))
	}

	// 检查是否是MOVED重定向
	if isMoved, slot, redirectAddr := proxy.protocol.IsMovedError(response); isMoved {
		LogInfo("收到MOVED重定向: slot=%s, 目标地址=%s", slot, redirectAddr)
		
		// 选择是否自动重定向还是返回重定向响应给客户端
		if proxy.shouldAutoRedirect(command) {
			// 自动重定向到正确的节点
			LogInfo("自动重定向到节点: %s", redirectAddr)
			return proxy.executeCommandWithRedirect(clientConn, command, redirectAddr, redirectCount+1)
		} else {
			// 直接返回重定向响应给客户端
			_, err = clientConn.Write([]byte(response))
			return err
		}
	}

	// 检查是否是ASK重定向
	if isAsk, slot, redirectAddr := proxy.protocol.IsAskError(response); isAsk {
		LogInfo("收到ASK重定向: slot=%s, 目标地址=%s", slot, redirectAddr)
		
		// ASK重定向通常需要先发送ASKING命令
		if proxy.shouldAutoRedirect(command) {
			LogInfo("自动处理ASK重定向到节点: %s", redirectAddr)
			return proxy.handleAskRedirect(clientConn, command, redirectAddr, redirectCount+1)
		} else {
			// 直接返回重定向响应给客户端
			_, err = clientConn.Write([]byte(response))
			return err
		}
	}

	// 普通响应，直接转发给客户端
	_, err = clientConn.Write([]byte(response))
	return err
}

// selectBackendNode 选择后端节点
func (proxy *RedisClusterProxy) selectBackendNode(command []string) string {
	if len(command) == 0 {
		return proxy.clusterManager.GetRandomNode()
	}

	cmdName := strings.ToUpper(command[0])
	
	// 根据命令类型选择节点
	switch cmdName {
	// 字符串操作命令
	case "GET", "SET", "GETSET", "SETNX", "SETEX", "PSETEX", "MGET", "MSET", "MSETNX",
		 "INCR", "DECR", "INCRBY", "DECRBY", "INCRBYFLOAT", "APPEND", "STRLEN",
		 "GETRANGE", "SETRANGE", "GETBIT", "SETBIT", "BITCOUNT", "BITOP":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 哈希操作命令
	case "HGET", "HSET", "HSETNX", "HMGET", "HMSET", "HGETALL", "HKEYS", "HVALS",
		 "HLEN", "HEXISTS", "HDEL", "HINCRBY", "HINCRBYFLOAT", "HSCAN":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 列表操作命令
	case "LPUSH", "RPUSH", "LPOP", "RPOP", "LLEN", "LRANGE", "LTRIM", "LINDEX",
		 "LSET", "LREM", "LINSERT", "BLPOP", "BRPOP", "BRPOPLPUSH", "RPOPLPUSH":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 集合操作命令
	case "SADD", "SREM", "SMEMBERS", "SCARD", "SISMEMBER", "SRANDMEMBER", "SPOP",
		 "SMOVE", "SINTER", "SINTERSTORE", "SUNION", "SUNIONSTORE", "SDIFF", "SDIFFSTORE", "SSCAN":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 有序集合操作命令
	case "ZADD", "ZREM", "ZSCORE", "ZINCRBY", "ZCARD", "ZCOUNT", "ZRANGE", "ZREVRANGE",
		 "ZRANGEBYSCORE", "ZREVRANGEBYSCORE", "ZRANK", "ZREVRANK", "ZREMRANGEBYRANK",
		 "ZREMRANGEBYSCORE", "ZUNIONSTORE", "ZINTERSTORE", "ZSCAN":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 通用key操作命令
	case "DEL", "EXISTS", "EXPIRE", "EXPIREAT", "TTL", "PTTL", "PERSIST", "TYPE",
		 "RENAME", "RENAMENX", "MOVE", "DUMP", "RESTORE", "SORT", "TOUCH":
		return proxy.selectNodeByKey(cmdName, command)
		
	// HyperLogLog命令
	case "PFADD", "PFCOUNT", "PFMERGE":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 位图操作命令
	case "BITFIELD":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 流操作命令
	case "XADD", "XREAD", "XREADGROUP", "XPENDING", "XCLAIM", "XACK", "XGROUP",
		 "XINFO", "XLEN", "XRANGE", "XREVRANGE", "XTRIM", "XDEL":
		return proxy.selectNodeByKey(cmdName, command)
		
	// 集群管理和信息命令
	case "CLUSTER", "INFO", "PING", "TIME", "COMMAND", "CONFIG", "CLIENT",
		 "MEMORY", "LATENCY", "SLOWLOG", "MONITOR", "DEBUG", "SHUTDOWN":
		// 这些命令可以发送到任意节点
		LogDebug("集群管理命令 %s 路由到随机节点", cmdName)
		return proxy.clusterManager.GetRandomNode()
		
	// 事务命令
	case "MULTI", "EXEC", "DISCARD", "WATCH", "UNWATCH":
		// 事务命令需要在同一个连接上执行，这里简化处理
		LogDebug("事务命令 %s 路由到随机节点", cmdName)
		return proxy.clusterManager.GetRandomNode()
		
	// 发布订阅命令
	case "PUBLISH", "SUBSCRIBE", "UNSUBSCRIBE", "PSUBSCRIBE", "PUNSUBSCRIBE", "PUBSUB":
		LogDebug("发布订阅命令 %s 路由到随机节点", cmdName)
		return proxy.clusterManager.GetRandomNode()
		
	// 脚本命令
	case "EVAL", "EVALSHA", "SCRIPT":
		// 脚本命令可能涉及多个key，这里简化处理
		LogDebug("脚本命令 %s 路由到随机节点", cmdName)
		return proxy.clusterManager.GetRandomNode()
		
	default:
		// 其他命令，发送到随机节点
		LogWarn("未知命令 %s，路由到随机节点", cmdName)
		return proxy.clusterManager.GetRandomNode()
	}
}

// selectNodeByKey 根据key选择节点
func (proxy *RedisClusterProxy) selectNodeByKey(cmdName string, command []string) string {
	if len(command) > 1 {
		key := command[1]
		nodeAddr := proxy.clusterManager.GetNodeForKey(key)
		if nodeAddr != "" {
			LogDebug("命令 %s key=%s 路由到节点: %s", cmdName, key, nodeAddr)
			return nodeAddr
		}
	}
	
	// 如果没有找到合适的节点，使用配置中的第一个节点
	if len(proxy.config.RedisNodes) > 0 {
		return proxy.config.RedisNodes[0]
	}
	
	return ""
}

// sendCommandToBackend 发送命令到后端Redis
func (proxy *RedisClusterProxy) sendCommandToBackend(conn net.Conn, command []string) error {
	// 构建Redis协议格式的命令
	var cmdBuilder strings.Builder
	cmdBuilder.WriteString(fmt.Sprintf("*%d\r\n", len(command)))
	
	for _, arg := range command {
		cmdBuilder.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(arg), arg))
	}

	_, err := conn.Write([]byte(cmdBuilder.String()))
	return err
}

// readBackendResponse 读取后端响应
func (proxy *RedisClusterProxy) readBackendResponse(conn net.Conn) (string, error) {
	// 设置读取超时，对于COMMAND命令需要更长的超时时间
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	defer conn.SetReadDeadline(time.Time{})

	reader := bufio.NewReader(conn)
	var response strings.Builder

	// 读取第一行
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取响应第一行失败: %v", err)
	}
	
	// 检查行是否为空或格式不正确
	if len(line) == 0 {
		return "", fmt.Errorf("收到空响应行")
	}
	
	// 添加调试日志
	LogDebug("收到后端响应第一行: %q (长度: %d)", line, len(line))
	
	response.WriteString(line)

	// 根据第一个字符判断响应类型
	switch line[0] {
	case '+', '-', ':':
		// 简单字符串、错误、整数 - 只有一行
		// 确保行以\r\n结尾
		if !strings.HasSuffix(line, "\r\n") {
			LogWarn("响应行不以\\r\\n结尾: %q", line)
		}
		return response.String(), nil
	case '$':
		// 批量字符串
		return proxy.readBulkStringResponse(reader, response.String())
	case '*':
		// 数组
		return proxy.readArrayResponse(reader, response.String())
	default:
		LogWarn("未知的响应类型字符: %c (ASCII: %d)", line[0], line[0])
		return response.String(), nil
	}
}

// readBulkStringResponse 读取批量字符串响应
func (proxy *RedisClusterProxy) readBulkStringResponse(reader *bufio.Reader, firstLine string) (string, error) {
	var response strings.Builder
	
	// 如果firstLine为空，需要先读取长度行
	if firstLine == "" {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		firstLine = line
	}
	
	response.WriteString(firstLine)

	// 检查firstLine长度，防止数组越界
	if len(firstLine) < 2 {
		return "", fmt.Errorf("无效的批量字符串响应格式: %s", firstLine)
	}

	// 解析长度
	lengthStr := strings.TrimSpace(firstLine[1:])
	if lengthStr == "-1" {
		return response.String(), nil // NULL
	}

	length := 0
	fmt.Sscanf(lengthStr, "%d", &length)

	if length > 0 {
		// 读取数据
		data := make([]byte, length+2) // +2 for \r\n
		_, err := io.ReadFull(reader, data)
		if err != nil {
			return "", err
		}
		response.Write(data)
	} else {
		// 读取空行
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		response.WriteString(line)
	}

	return response.String(), nil
}

// readArrayResponse 读取数组响应
func (proxy *RedisClusterProxy) readArrayResponse(reader *bufio.Reader, firstLine string) (string, error) {
	var response strings.Builder
	response.WriteString(firstLine)

	// 检查firstLine长度，防止数组越界
	if len(firstLine) < 2 {
		return "", fmt.Errorf("无效的数组响应格式: %s", firstLine)
	}

	// 解析数组长度
	countStr := strings.TrimSpace(firstLine[1:])
	if countStr == "-1" {
		return response.String(), nil // NULL数组
	}

	count := 0
	fmt.Sscanf(countStr, "%d", &count)
	
	LogDebug("开始读取数组响应，元素数量: %d", count)

	// 读取数组元素
	for i := 0; i < count; i++ {
		// 对于大数组，每100个元素打印一次进度
		if count > 100 && i%100 == 0 {
			LogDebug("读取数组进度: %d/%d", i, count)
		}
		
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("读取数组元素 %d/%d 失败: %v", i+1, count, err)
		}
		response.WriteString(line)

		// 检查line长度，防止数组越界
		if len(line) == 0 {
			continue
		}

		// 根据元素类型读取额外数据
		switch line[0] {
		case '$':
			// 批量字符串元素，传入当前行作为firstLine
			elementResponse, err := proxy.readBulkStringResponse(reader, line)
			if err != nil {
				return "", fmt.Errorf("读取批量字符串元素 %d/%d 失败: %v", i+1, count, err)
			}
			// 不需要再次添加line，因为readBulkStringResponse已经包含了
			response.WriteString(elementResponse[len(line):])
		case '*':
			// 嵌套数组元素
			elementResponse, err := proxy.readArrayResponse(reader, line)
			if err != nil {
				return "", fmt.Errorf("读取嵌套数组元素 %d/%d 失败: %v", i+1, count, err)
			}
			// 不需要再次添加line，因为readArrayResponse已经包含了
			response.WriteString(elementResponse[len(line):])
		}
	}
	
	LogDebug("数组响应读取完成，总元素数: %d，响应长度: %d", count, response.Len())

	return response.String(), nil
}

// shouldAutoRedirect 判断是否应该自动重定向
func (proxy *RedisClusterProxy) shouldAutoRedirect(command []string) bool {
	// 使用配置中的AutoRedirect选项
	if !proxy.config.AutoRedirect {
		return false
	}
	
	// 某些命令可能不适合自动重定向，比如CLUSTER相关命令
	if len(command) > 0 {
		cmd := strings.ToUpper(command[0])
		switch cmd {
		case "CLUSTER", "INFO", "PING", "COMMAND":
			return false // 这些命令不需要自动重定向
		}
	}
	
	return true
}

// handleAskRedirect 处理ASK重定向
func (proxy *RedisClusterProxy) handleAskRedirect(clientConn net.Conn, command []string, redirectAddr string, redirectCount int) error {
	// 获取后端连接
	backendConn, err := proxy.pool.GetConnection(redirectAddr)
	if err != nil {
		return fmt.Errorf("连接重定向节点失败: %v", err)
	}
	defer proxy.pool.ReturnConnection(redirectAddr, backendConn)

	// 发送ASKING命令
	_, err = backendConn.Write([]byte("ASKING\r\n"))
	if err != nil {
		return fmt.Errorf("发送ASKING命令失败: %v", err)
	}

	// 读取ASKING响应
	reader := bufio.NewReader(backendConn)
	askingResponse, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("读取ASKING响应失败: %v", err)
	}

	if !strings.HasPrefix(askingResponse, "+OK") {
		return fmt.Errorf("ASKING命令响应错误: %s", askingResponse)
	}

	// 发送原始命令
	err = proxy.sendCommandToBackend(backendConn, command)
	if err != nil {
		return fmt.Errorf("发送命令到重定向节点失败: %v", err)
	}

	// 读取响应并转发给客户端
	response, err := proxy.readBackendResponse(backendConn)
	if err != nil {
		return fmt.Errorf("读取重定向节点响应失败: %v", err)
	}

	_, err = clientConn.Write([]byte(response))
	return err
}

// sendError 发送错误响应
func (proxy *RedisClusterProxy) sendError(conn net.Conn, message string) {
	errorResponse := proxy.protocol.FormatError(message)
	conn.Write([]byte(errorResponse))
}