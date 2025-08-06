#!/bin/bash

# Redis集群代理交叉编译脚本
# 支持编译多个平台的可执行文件

echo "开始编译Redis集群代理..."

# 清理旧的构建文件
rm -f redis-cluster-proxy-*

# 编译Linux AMD64版本（最常用）
echo "编译Linux AMD64版本..."
GOOS=linux GOARCH=amd64 go build -o redis-cluster-proxy-linux-amd64
if [ $? -eq 0 ]; then
    echo "✅ Linux AMD64版本编译成功"
else
    echo "❌ Linux AMD64版本编译失败"
    exit 1
fi

# 编译Linux ARM64版本（ARM服务器）
echo "编译Linux ARM64版本..."
GOOS=linux GOARCH=arm64 go build -o redis-cluster-proxy-linux-arm64
if [ $? -eq 0 ]; then
    echo "✅ Linux ARM64版本编译成功"
else
    echo "❌ Linux ARM64版本编译失败"
fi

# 编译Windows版本
echo "编译Windows AMD64版本..."
GOOS=windows GOARCH=amd64 go build -o redis-cluster-proxy-windows.exe
if [ $? -eq 0 ]; then
    echo "✅ Windows AMD64版本编译成功"
else
    echo "❌ Windows AMD64版本编译失败"
fi

# 编译macOS版本
echo "编译macOS AMD64版本..."
GOOS=darwin GOARCH=amd64 go build -o redis-cluster-proxy-macos-amd64
if [ $? -eq 0 ]; then
    echo "✅ macOS AMD64版本编译成功"
else
    echo "❌ macOS AMD64版本编译失败"
fi

# 编译macOS ARM64版本（M1/M2 Mac）
echo "编译macOS ARM64版本..."
GOOS=darwin GOARCH=arm64 go build -o redis-cluster-proxy-macos-arm64
if [ $? -eq 0 ]; then
    echo "✅ macOS ARM64版本编译成功"
else
    echo "❌ macOS ARM64版本编译失败"
fi

echo ""
echo "编译完成！生成的文件："
ls -la redis-cluster-proxy-*

echo ""
echo "使用说明："
echo "- Linux服务器(x86_64): redis-cluster-proxy-linux-amd64"
echo "- Linux服务器(ARM64):  redis-cluster-proxy-linux-arm64"
echo "- Windows系统:         redis-cluster-proxy-windows.exe"
echo "- macOS Intel:         redis-cluster-proxy-macos-amd64"
echo "- macOS M1/M2:         redis-cluster-proxy-macos-arm64"