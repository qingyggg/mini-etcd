// Package main 为 mini-etcd API 提供一个简单的命令行客户端。
// 此客户端演示如何与 etcd 兼容的 HTTP API 交互。
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
)

// EtcdClient 为与 mini-etcd 交互提供了一个简单的接口。
// 它将 HTTP 请求包装在便于常见操作的方法中。
type EtcdClient struct {
	baseURL string      // etcd 服务器的基本 URL（例如“http://localhost:8081”）
	client  *http.Client // 用于发送请求的 HTTP 客户端
}

// NewEtcdClient 为给定的 etcd 服务器 URL 创建一个新客户端。
// 客户端使用默认的 HTTP 客户端来发送请求。
func NewEtcdClient(baseURL string) *EtcdClient {
	return &EtcdClient{
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

// Set 在 etcd 服务器中存储键值对。
// 这向 /v2/keys/{key} 端点发送 PUT 请求。
func (c *EtcdClient) Set(key, value string) error {
	data := url.Values{}
	data.Set("value", value)

	resp, err := c.client.PostForm(fmt.Sprintf("%s/v2/keys/%s", c.baseURL, key), data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Get 从 etcd 服务器检索给定键的值。
// 这向 /v2/keys/{key} 端点发送 GET 请求。
func (c *EtcdClient) Get(key string) (string, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/v2/keys/%s", c.baseURL, key))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("key not found")
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var response map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", err
	}

	if node, ok := response["node"].(map[string]any); ok {
		if value, ok := node["value"].(string); ok {
			return value, nil
		}
	}

	return "", fmt.Errorf("unexpected response format")
}

// Delete 从 etcd 服务器中删除一个键。
// 这向 /v2/keys/{key} 端点发送 DELETE 请求。
func (c *EtcdClient) Delete(key string) error {
	req, err := http.NewRequest("DELETE", fmt.Sprintf("%s/v2/keys/%s", c.baseURL, key), nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// main 是客户端应用程序的入口点。
// 它解析命令行参数并执行请求的操作。
func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run client.go <command> [args...]")
		fmt.Println("Commands:")
		fmt.Println("  set <key> <value>  - Set a key-value pair")
		fmt.Println("  get <key>          - Get a value by key")
		fmt.Println("  del <key>          - Delete a key")
		os.Exit(1)
	}

	client := NewEtcdClient("http://localhost:8081")
	command := os.Args[1]

	switch command {
	case "set":
		if len(os.Args) < 4 {
			fmt.Println("Usage: set <key> <value>")
			os.Exit(1)
		}
		key := os.Args[2]
		value := os.Args[3]
		if err := client.Set(key, value); err != nil {
			fmt.Printf("Error setting key: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Set %s = %s\n", key, value)

	case "get":
		if len(os.Args) < 3 {
			fmt.Println("Usage: get <key>")
			os.Exit(1)
		}
		key := os.Args[2]
		value, err := client.Get(key)
		if err != nil {
			fmt.Printf("Error getting key: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("%s = %s\n", key, value)

	case "del":
		if len(os.Args) < 3 {
			fmt.Println("Usage: del <key>")
			os.Exit(1)
		}
		key := os.Args[2]
		if err := client.Delete(key); err != nil {
			fmt.Printf("Error deleting key: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Deleted %s\n", key)

	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}
}