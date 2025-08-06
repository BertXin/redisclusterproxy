# Redis集群代理服务

## 项目简介

这是一个高性能的Redis集群代理服务，提供智能路由、自动重定向和集群感知功能。代理服务直接连接Redis集群节点，为客户端提供简化的单点访问接口。

## 核心特性

1. **智能路由**: 基于Redis Cluster slot算法的精确路由
2. **自动重定向**: 透明处理MOVED/ASK重定向，客户端无感知
3. **集群感知**: 自动发现和管理集群拓扑变化
4. **高性能**: 连接池管理，支持高并发访问
5. **简化部署**: 无需复杂的负载均衡器配置

## 项目结构

```
├── main.go          # 主程序入口
├── config.go        # 配置管理
├── proxy.go         # 代理服务器核心逻辑
├── protocol.go      # Redis协议解析
├── pool.go          # 连接池管理
├── config.yaml      # 配置文件示例
└── README.md        # 说明文档
```

## 配置说明

### 1. 修改配置

在`main.go`中修改以下配置：

```go
// 修改为您的实际Redis集群节点地址
config := &Config{
    ProxyPort: 6379,  // 代理监听端口
    RedisNodes: []string{
        "redis-node-1:6379",  // 替换为实际的Redis节点地址
        "redis-node-2:6379",
        "redis-node-3:6379",
        "redis-node-4:6379",
        "redis-node-5:6379",
        "redis-node-6:6379",
    },
    AutoRedirect: true,  // 启用自动重定向
}
```

或者使用YAML配置文件：

```yaml
proxy_port: 6379
redis_nodes:
  - "redis-node-1.example.com:6379"
  - "redis-node-2.example.com:6379"
  - "redis-node-3.example.com:6379"
  - "redis-node-4.example.com:6379"
  - "redis-node-5.example.com:6379"
  - "redis-node-6.example.com:6379"
auto_redirect: true
```

### 2. 配置说明

- `proxy_port`: 代理服务监听端口，客户端连接此端口
- `redis_nodes`: Redis集群节点地址列表，代理会自动发现完整集群拓扑
- `auto_redirect`: 是否启用自动重定向功能

**注意**: 
- 确保代理服务器能够直接访问所有Redis集群节点
- 代理会自动发现集群拓扑，只需配置部分节点即可
- 启用`auto_redirect`可以让客户端无需处理集群重定向

## 使用方法

### 1. 启动代理服务

```bash
# 编译项目
go build -o redis-cluster-proxy

# 启动代理服务
./redis-cluster-proxy
```

### 2. 客户端连接

客户端可以像连接单个Redis实例一样连接代理：

```bash
# 使用redis-cli连接代理
redis-cli -h proxy-host -p 6379

# 或者使用集群模式（推荐）
redis-cli -h proxy-host -p 6379 -c
```

### 3. 测试验证

```bash
# 设置测试数据
SET user:1001 "Alice"
SET user:1002 "Bob"
SET product:2001 "Laptop"

# 获取数据
GET user:1001
GET user:1002
GET product:2001

# 测试Hash Tag功能
SET {user:1001}:profile "Engineer"
SET {user:1001}:settings "theme:dark"

# 查看集群信息
CLUSTER NODES
CLUSTER INFO
```

### 3. 程序化连接

```python
import redis

# Python客户端示例
r = redis.Redis(host='your-proxy-host', port=7000, decode_responses=True)
r.set('key1', 'value1')
print(r.get('key1'))
```

## 核心功能

1. **智能路由**: 基于Redis Cluster slot算法的智能命令路由
2. **自动重定向**: 可配置的自动MOVED/ASK重定向处理，无需客户端感知
3. **集群感知**: 自动发现和管理Redis集群拓扑，支持动态更新
4. **MOVED重定向处理**: 自动拦截Redis集群的MOVED响应，将内网地址重写为公网地址
5. **ASK重定向处理**: 处理集群重新分片期间的ASK重定向，支持ASKING命令
6. **连接池管理**: 维护到后端Redis节点的连接池，提高性能
7. **负载均衡**: 在多个Redis节点间分发请求
8. **地址映射**: 灵活的内网到公网地址映射配置

### 详细功能说明

#### 1. 智能路由

代理服务实现了完整的Redis Cluster路由逻辑：

- **Slot计算**: 使用CRC16算法计算key对应的slot (0-16383)
- **Hash Tag支持**: 支持`{key}`格式的hash tag，确保相关key路由到同一节点
- **命令分类路由**:
  - 单key命令 (GET, SET, DEL等): 基于key的slot路由
  - 多key命令 (MGET, MSET等): 使用第一个key路由
  - 集群命令 (CLUSTER, INFO等): 路由到随机节点
- **集群拓扑感知**: 自动发现集群节点和slot分布，每30秒刷新

#### 2. 自动重定向

当启用`auto_redirect: true`时，代理会自动处理重定向：

- **MOVED重定向**: 自动处理slot迁移场景，透明重定向到正确节点
- **ASK重定向**: 处理临时重定向请求，支持数据迁移期间的访问
- **重定向限制**: 防止无限重定向循环，保护系统稳定性
- **透明处理**: 客户端无需感知重定向过程，简化应用开发

#### 3. MOVED重定向处理

当Redis返回MOVED重定向时：

```
-MOVED 3999 redis-node-2:6379
```

代理会：
1. 解析重定向信息，获取目标节点地址
2. 自动向目标节点重新发送命令
3. 更新内部slot映射表
4. 直接返回目标节点的响应给客户端

客户端完全无需感知重定向过程，就像访问单个Redis实例一样。

#### 4. ASK重定向处理

当Redis返回ASK重定向时：

```
-ASK 3999 redis-node-3:6379
```

代理会：
1. 解析ASK重定向信息
2. 向目标节点发送ASKING命令
3. 重新发送原始命令到目标节点
4. 返回目标节点的响应给客户端

ASK重定向通常发生在slot迁移过程中，代理会自动处理这种临时重定向。

#### 3. 连接池管理

- 维护到后端Redis节点的连接池
- 自动重连和健康检查
- 连接复用提高性能

## 部署建议

### 1. 生产环境部署

```bash
# 使用systemd管理服务
sudo cp redis-cluster-proxy /usr/local/bin/
sudo cp redis-cluster-proxy.service /etc/systemd/system/

# 启动服务
sudo systemctl enable redis-cluster-proxy
sudo systemctl start redis-cluster-proxy
```

### 2. Docker部署

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o redis-cluster-proxy

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/redis-cluster-proxy .
EXPOSE 7000
CMD ["./redis-cluster-proxy"]
```

## 监控和日志

代理服务会输出详细的日志信息：
- 客户端连接/断开
- 命令转发
- MOVED/ASK重定向处理
- 错误信息

建议在生产环境中配置日志收集和监控。

## 注意事项

1. **网络连通性**: 代理服务器必须能够直接访问所有Redis集群节点
2. **集群状态**: Redis集群必须处于正常状态，所有节点可达
3. **节点发现**: 代理会自动发现集群拓扑，配置文件中只需包含部分节点
4. **端口配置**: 确保所有Redis节点使用相同的端口，或在配置中明确指定
5. **防火墙设置**: 确保代理服务器到Redis节点的网络路径畅通
6. **性能考虑**: 代理会增加一定的延迟，建议部署在与Redis集群相同的网络环境中
7. **高可用**: 可以部署多个代理实例，通过负载均衡器提供高可用性
8. **安全性**: 确保代理服务的访问控制和网络安全

## 故障排除

### 1. 连接失败
- 检查Redis集群节点是否可达
- 验证网络连通性和防火墙设置
- 确认Redis节点配置是否正确

### 2. 集群发现问题
- 检查配置的Redis节点是否为集群模式
- 验证CLUSTER NODES命令是否正常返回
- 确认集群状态是否健康

### 3. 重定向处理问题
- 检查AutoRedirect配置是否启用
- 验证集群slot分布是否正常
- 确认节点间网络连通性

### 4. 性能问题
- 检查网络延迟
- 调整连接池大小
- 监控代理服务器资源使用情况
- 考虑部署多个代理实例

## 扩展功能

可以根据需要添加以下功能：
- 完整的YAML配置文件支持
- 更智能的负载均衡策略
- 监控指标暴露（Prometheus）
- 管理API接口
- 配置热重载

## 许可证

MIT License