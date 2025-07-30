// Package raft 为 Raft 节点提供持久化功能。
// 此文件处理将 Raft 节点状态保存到磁盘和从磁盘恢复。
package raft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"mini-etcd/pkg/types"
)

// PersistentState 表示必须持久化到磁盘的 Raft 状态。
// 根据 Raft 论文，这些字段必须在响应 RPC 之前持久化。
type PersistentState struct {
	CurrentTerm uint64           `json:"current_term"` // 服务器见过的最新任期
	VotedFor    *types.NodeID    `json:"voted_for"`    // 当前任期内收到投票的候选者 ID
	Log         []types.LogEntry `json:"log"`          // 日志条目
}

// persist 将当前 Raft 状态保存到磁盘。
// 这确保关键状态在节点重启后存活并维持 Raft 安全属性。
func (n *Node) persist() error {
	if n.config.DataDir == "" {
		return nil
	}

	state := &PersistentState{
		CurrentTerm: n.currentTerm,
		VotedFor:    n.votedFor,
		Log:         n.log,
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %v", err)
	}

	if err := os.MkdirAll(n.config.DataDir, 0755); err != nil {
		return fmt.Errorf("failed to create data directory: %v", err)
	}

	statePath := filepath.Join(n.config.DataDir, fmt.Sprintf("raft-state-%s.json", n.id))
	tmpPath := statePath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %v", err)
	}

	if err := os.Rename(tmpPath, statePath); err != nil {
		return fmt.Errorf("failed to rename state file: %v", err)
	}

	return nil
}

// restore 从磁盘加载之前保存的 Raft 状态。
// 这在节点启动期间调用以从之前的会话中恢复。
func (n *Node) restore() error {
	if n.config.DataDir == "" {
		return nil
	}

	statePath := filepath.Join(n.config.DataDir, fmt.Sprintf("raft-state-%s.json", n.id))
	
	data, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read state file: %v", err)
	}

	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %v", err)
	}

	n.currentTerm = state.CurrentTerm
	n.votedFor = state.VotedFor
	n.log = state.Log

	return nil
}