// Package tests 包含 mini-etcd 实现的基本单元测试。
// 这些测试验证键值存储的核心功能。
package tests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"mini-etcd/internal/store"
	"mini-etcd/pkg/types"
)

// TestStore 测试键值存储的基本功能。
// 它验证 PUT、GET、DELETE 操作和 TTL 功能。
func TestStore(t *testing.T) {
	s := store.NewStore()

	// 测试基本的 PUT 和 GET 操作
	t.Run("存储和获取", func(t *testing.T) {
		entry := types.LogEntry{
			Index:   1,
			Term:    1,
			Command: []byte(`{"type":"PUT","key":"foo","value":"bar"}`),
		}

		err := s.Apply(entry)
		assert.NoError(t, err)

		value, exists := s.Get("foo")
		assert.True(t, exists)
		assert.Equal(t, "bar", value)
	})

	// 测试 DELETE 操作
	t.Run("删除", func(t *testing.T) {
		entry := types.LogEntry{
			Index:   2,
			Term:    1,
			Command: []byte(`{"type":"DELETE","key":"foo"}`),
		}

		err := s.Apply(entry)
		assert.NoError(t, err)

		_, exists := s.Get("foo")
		assert.False(t, exists)
	})

	// 测试 TTL（生存时间）功能
	t.Run("TTL", func(t *testing.T) {
		entry := types.LogEntry{
			Index:   3,
			Term:    1,
			Command: []byte(`{"type":"PUT","key":"temp","value":"test","ttl":1}`),
		}

		err := s.Apply(entry)
		assert.NoError(t, err)

		value, exists := s.Get("temp")
		assert.True(t, exists)
		assert.Equal(t, "test", value)

		time.Sleep(2 * time.Second)

		_, exists = s.Get("temp")
		assert.False(t, exists)
	})
}