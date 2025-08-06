package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RedisProtocol Redis协议解析器
type RedisProtocol struct{}

// ParseCommand 解析Redis命令
func (rp *RedisProtocol) ParseCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return nil, fmt.Errorf("空命令")
	}

	// 处理数组格式 *<count>\r\n
	if line[0] == '*' {
		return rp.parseArrayCommand(reader, line)
	}

	// 处理简单字符串格式
	return strings.Fields(line), nil
}

// parseArrayCommand 解析数组格式的命令
func (rp *RedisProtocol) parseArrayCommand(reader *bufio.Reader, firstLine string) ([]string, error) {
	countStr := firstLine[1:]
	count, err := strconv.Atoi(countStr)
	if err != nil {
		return nil, fmt.Errorf("无效的数组长度: %s", countStr)
	}

	if count <= 0 {
		return []string{}, nil
	}

	args := make([]string, count)
	for i := 0; i < count; i++ {
		arg, err := rp.parseBulkString(reader)
		if err != nil {
			return nil, err
		}
		args[i] = arg
	}

	return args, nil
}

// parseBulkString 解析批量字符串
func (rp *RedisProtocol) parseBulkString(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	line = strings.TrimSpace(line)
	if len(line) == 0 || line[0] != '$' {
		return "", fmt.Errorf("无效的批量字符串格式")
	}

	lengthStr := line[1:]
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return "", fmt.Errorf("无效的字符串长度: %s", lengthStr)
	}

	if length == -1 {
		return "", nil // NULL
	}

	if length == 0 {
		// 读取空行
		reader.ReadString('\n')
		return "", nil
	}

	// 读取指定长度的数据
	data := make([]byte, length)
	_, err = io.ReadFull(reader, data)
	if err != nil {
		return "", err
	}

	// 读取结尾的\r\n
	reader.ReadString('\n')

	return string(data), nil
}

// FormatResponse 格式化Redis响应
func (rp *RedisProtocol) FormatResponse(response string) string {
	return response
}

// FormatError 格式化错误响应
func (rp *RedisProtocol) FormatError(message string) string {
	return fmt.Sprintf("-ERR %s\r\n", message)
}

// FormatSimpleString 格式化简单字符串响应
func (rp *RedisProtocol) FormatSimpleString(message string) string {
	return fmt.Sprintf("+%s\r\n", message)
}

// IsMovedError 检查是否是MOVED错误
func (rp *RedisProtocol) IsMovedError(response string) (bool, string, string) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "-MOVED ") {
		parts := strings.Fields(response)
		if len(parts) >= 3 {
			slot := parts[1]
			address := parts[2]
			return true, slot, address
		}
	}
	return false, "", ""
}

// IsAskError 检查是否是ASK错误
func (rp *RedisProtocol) IsAskError(response string) (bool, string, string) {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "-ASK ") {
		parts := strings.Fields(response)
		if len(parts) >= 3 {
			slot := parts[1]
			address := parts[2]
			return true, slot, address
		}
	}
	return false, "", ""
}

// RewriteMovedResponse 重写MOVED响应中的地址
func (rp *RedisProtocol) RewriteMovedResponse(response string, newAddress string) string {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "-MOVED ") {
		parts := strings.Fields(response)
		if len(parts) >= 3 {
			parts[2] = newAddress
			return strings.Join(parts, " ") + "\r\n"
		}
	}
	return response
}

// RewriteAskResponse 重写ASK响应中的地址
func (rp *RedisProtocol) RewriteAskResponse(response string, newAddress string) string {
	response = strings.TrimSpace(response)
	if strings.HasPrefix(response, "-ASK ") {
		parts := strings.Fields(response)
		if len(parts) >= 3 {
			parts[2] = newAddress
			return strings.Join(parts, " ") + "\r\n"
		}
	}
	return response
}