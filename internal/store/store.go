// Package store 实现了作为 Raft 共识算法状态机的键值存储。
// 它提供支持 TTL 的线程安全操作。
package store

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"mini-etcd/pkg/types"
)

// Store 表示作为 Raft 状态机的键值存储。
// 它支持基本操作（GET、PUT、DELETE）和可选的键 TTL。
type Store struct {
	mu   sync.RWMutex         // 保护对 data 和 ttl 映射的并发访问
	data map[string]string   // 主键值存储
	ttl  map[string]time.Time // 键的 TTL 过期时间
}

// KVRequest 表示键值操作请求。
// 这是通过 Raft 日志复制的命令格式。
type KVRequest struct {
	Type  string `json:"type"`            // 操作类型："PUT" 或 "DELETE"
	Key   string `json:"key"`             // 要操作的键
	Value string `json:"value,omitempty"` // PUT 操作的值
	TTL   int64  `json:"ttl,omitempty"`   // 生存时间（秒）（0 表示永不过期）
}

// KVResponse 表示键值操作的响应。
// API 层使用它向客户端返回结果。
type KVResponse struct {
	Success bool   `json:"success"`          // 操作是否成功
	Value   string `json:"value,omitempty"`  // 成功 GET 操作的值
	Error   string `json:"error,omitempty"`  // 失败操作的错误消息
}

// NewStore 创建一个带有空数据的新键值存储。
// 它还启动一个后台 goroutine 来清理过期的键。
func NewStore() *Store {
	s := &Store{
		data: make(map[string]string),
		ttl:  make(map[string]time.Time),
	}
	go s.cleanupExpired()
	return s
}

// Apply 处理来自 Raft 算法的已提交日志条目。
// 这是状态机实际执行复制命令的地方。
func (s *Store) Apply(entry types.LogEntry) error {
	data, ok := entry.Command.([]byte)
	if !ok {
		return fmt.Errorf("invalid command type")
	}

	var req KVRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("failed to unmarshal command: %v", err)
	}

	switch req.Type {
	case "PUT":
		return s.put(req.Key, req.Value, req.TTL)
	case "DELETE":
		return s.delete(req.Key)
	default:
		return fmt.Errorf("unknown command type: %s", req.Type)
	}
}

// Get 从存储中检索给定键的值。
// 它返回值和一个布尔值，指示键是否存在且没有过期。
func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if expiry, exists := s.ttl[key]; exists && time.Now().After(expiry) {
		delete(s.data, key)
		delete(s.ttl, key)
		return "", false
	}

	value, exists := s.data[key]
	return value, exists
}

// put 存储带有可选 TTL 的键值对。
// 这是在 Raft 共识后通过 Apply() 调用的内部方法。
func (s *Store) put(key, value string, ttlSeconds int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data[key] = value

	if ttlSeconds > 0 {
		s.ttl[key] = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	} else {
		delete(s.ttl, key)
	}

	return nil
}

// delete 从存储中删除键值对。
// 这是在 Raft 共识后通过 Apply() 调用的内部方法。
func (s *Store) delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, key)
	delete(s.ttl, key)
	return nil
}

// List 返回存储中所有未过期键值对的副本。
// 这对于调试和管理操作很有用。
func (s *Store) List() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	now := time.Now()

	for key, value := range s.data {
		if expiry, exists := s.ttl[key]; !exists || now.Before(expiry) {
			result[key] = value
		}
	}

	return result
}

// cleanupExpired 在后台 goroutine 中运行以删除过期的键。
// 它每分钟检查一次过期的键以保持存储清洁。
func (s *Store) cleanupExpired() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, expiry := range s.ttl {
			if now.After(expiry) {
				delete(s.data, key)
				delete(s.ttl, key)
			}
		}
		s.mu.Unlock()
	}
}

// Size 返回存储中未过期键的数量。
// 这不包括已过期但尚未清理的键。
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	count := 0

	for key := range s.data {
		if expiry, exists := s.ttl[key]; !exists || now.Before(expiry) {
			count++
		}
	}

	return count
}