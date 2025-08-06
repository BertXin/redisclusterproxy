package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"gopkg.in/yaml.v3"
)

func main() {
	// 解析命令行参数
	configFile := flag.String("config", "config.yaml", "配置文件路径")
	flag.Parse()

	// 加载配置
	config, err := LoadConfigFromFile(*configFile)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 验证配置
	if err := config.ValidateConfig(); err != nil {
		log.Fatalf("配置验证失败: %v", err)
	}

	// 初始化日志系统
	InitLogger(config.LogLevel, config.LogFile)
	if config.LogFile != "" {
		LogInfo("日志系统已初始化，级别: %s，文件: %s", config.LogLevel, config.LogFile)
	} else {
		LogInfo("日志系统已初始化，级别: %s，输出到控制台", config.LogLevel)
	}

	// 创建代理服务
	proxy := NewRedisClusterProxy(config)

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动代理服务
	go func() {
		if err := proxy.Start(); err != nil {
			log.Fatalf("启动代理服务失败: %v", err)
		}
	}()

	// 等待退出信号
	<-sigChan
	log.Println("收到退出信号，正在关闭代理服务...")
	proxy.Stop()
	log.Println("代理服务已关闭")
	
	// 关闭日志文件
	CloseLogger()
}

// LoadConfigFromFile 从YAML文件加载配置
func LoadConfigFromFile(filename string) (*Config, error) {
	// 默认配置
	config := &Config{
		ProxyPort: 6379,
		RedisNodes: []string{
			"127.0.0.1:7000",
			"127.0.0.1:7001", 
			"127.0.0.1:7002",
		},
		AutoRedirect: true,
		LogLevel: "info",
		LogFile: "", // 默认输出到控制台
	}

	// 检查配置文件是否存在
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		log.Printf("配置文件 %s 不存在，使用默认配置", filename)
		log.Printf("默认配置: ProxyPort=%d, RedisNodes=%v, AutoRedirect=%t", 
			config.ProxyPort, config.RedisNodes, config.AutoRedirect)
		log.Printf("提示: 可以创建 %s 文件来自定义配置", filename)
		return config, nil
	}

	// 读取配置文件
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Printf("读取配置文件失败: %v，使用默认配置", err)
		return config, nil
	}

	// 解析YAML配置
	if err := yaml.Unmarshal(data, config); err != nil {
		log.Printf("解析配置文件失败: %v，使用默认配置", err)
		return config, nil
	}

	log.Printf("成功加载配置文件: %s", filename)
	log.Printf("配置: ProxyPort=%d, RedisNodes=%v, AutoRedirect=%t", 
		config.ProxyPort, config.RedisNodes, config.AutoRedirect)
	
	return config, nil
}