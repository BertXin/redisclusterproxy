package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// ConnectionPool Redis连接池
type ConnectionPool struct {
	pools map[string]*NodePool
	mutex sync.RWMutex
}

// NodePool 单个节点的连接池
type NodePool struct {
	address     string
	connections chan net.Conn
	maxSize     int
	currentSize int
	mutex       sync.Mutex
}

// NewConnectionPool 创建新的连接池
func NewConnectionPool() *ConnectionPool {
	return &ConnectionPool{
		pools: make(map[string]*NodePool),
	}
}

// GetConnection 获取到指定地址的连接
func (cp *ConnectionPool) GetConnection(address string) (net.Conn, error) {
	cp.mutex.RLock()
	pool, exists := cp.pools[address]
	cp.mutex.RUnlock()

	if !exists {
		cp.mutex.Lock()
		// 双重检查
		if pool, exists = cp.pools[address]; !exists {
			pool = &NodePool{
				address:     address,
				connections: make(chan net.Conn, 10),
				maxSize:     10,
			}
			cp.pools[address] = pool
		}
		cp.mutex.Unlock()
	}

	return pool.GetConnection()
}

// ReturnConnection 归还连接到池中
func (cp *ConnectionPool) ReturnConnection(address string, conn net.Conn) {
	cp.mutex.RLock()
	pool, exists := cp.pools[address]
	cp.mutex.RUnlock()

	if exists {
		pool.ReturnConnection(conn)
	} else {
		conn.Close()
	}
}

// GetConnection 从节点池获取连接
func (np *NodePool) GetConnection() (net.Conn, error) {
	select {
	case conn := <-np.connections:
		// 检查连接是否仍然有效
		if np.isConnectionValid(conn) {
			return conn, nil
		}
		// 连接无效，创建新连接
		return np.createConnection()
	default:
		// 池中没有可用连接，创建新连接
		return np.createConnection()
	}
}

// ReturnConnection 归还连接到节点池
func (np *NodePool) ReturnConnection(conn net.Conn) {
	if conn == nil {
		return
	}

	select {
	case np.connections <- conn:
		// 成功归还到池中
	default:
		// 池已满，关闭连接
		conn.Close()
		np.mutex.Lock()
		np.currentSize--
		np.mutex.Unlock()
	}
}

// createConnection 创建新的连接
func (np *NodePool) createConnection() (net.Conn, error) {
	np.mutex.Lock()
	defer np.mutex.Unlock()

	if np.currentSize >= np.maxSize {
		return nil, fmt.Errorf("连接池已满")
	}

	conn, err := net.DialTimeout("tcp", np.address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("连接Redis节点失败 %s: %v", np.address, err)
	}

	np.currentSize++
	return conn, nil
}

// isConnectionValid 检查连接是否有效
func (np *NodePool) isConnectionValid(conn net.Conn) bool {
	if conn == nil {
		return false
	}

	// 设置读取超时
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	defer conn.SetReadDeadline(time.Time{})

	// 尝试发送PING命令
	_, err := conn.Write([]byte("PING\r\n"))
	if err != nil {
		return false
	}

	// 读取响应
	buffer := make([]byte, 7) // +PONG\r\n
	_, err = conn.Read(buffer)
	return err == nil
}

// Close 关闭连接池
func (cp *ConnectionPool) Close() {
	cp.mutex.Lock()
	defer cp.mutex.Unlock()

	for _, pool := range cp.pools {
		pool.Close()
	}
	cp.pools = make(map[string]*NodePool)
}

// Close 关闭节点池
func (np *NodePool) Close() {
	close(np.connections)
	for conn := range np.connections {
		if conn != nil {
			conn.Close()
		}
	}
}