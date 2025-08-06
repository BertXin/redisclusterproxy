package main

import (
	"fmt"
	"net"
)

// Config 代理配置
type Config struct {
	ProxyPort    int      `yaml:"proxy_port"`    // 代理监听端口
	RedisNodes   []string `yaml:"redis_nodes"`   // Redis集群节点地址列表
	AutoRedirect bool     `yaml:"auto_redirect"` // 是否自动处理重定向
	LogLevel     string   `yaml:"log_level"`     // 日志级别: debug, info, warn, error
	LogFile      string   `yaml:"log_file"`      // 日志文件路径，为空则输出到控制台
}

// LoadConfig 加载配置文件（在main.go中实现）

// GetRedisNodes 获取Redis节点列表
func (c *Config) GetRedisNodes() []string {
	return c.RedisNodes
}

// GetProxyAddress 获取代理服务地址
func (c *Config) GetProxyAddress() string {
	return fmt.Sprintf(":%d", c.ProxyPort)
}

// 注意：已移除MapAddress方法，因为直接连接Redis节点，不需要地址映射

// ValidateConfig 验证配置
func (c *Config) ValidateConfig() error {
	if len(c.RedisNodes) == 0 {
		return fmt.Errorf("Redis节点列表不能为空")
	}

	for _, node := range c.RedisNodes {
		if _, _, err := net.SplitHostPort(node); err != nil {
			return fmt.Errorf("无效的Redis节点地址: %s", node)
		}
	}

	return nil
}