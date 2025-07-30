// Package types 定义了整个 mini-etcd 实现中使用的核心数据结构和类型。
// 包括 Raft 特定类型、配置结构和命令定义。
package types

import (
	"encoding/json"
	"time"
)

// NodeState 表示集群中 Raft 节点的当前状态。
// 根据 Raft 算法定义，每个节点可以处于三种状态之一。
type NodeState int

// Raft 共识算法定义的节点状态：
// - Follower: 默认状态，接受 AppendEntries 并为候选者投票
// - Candidate: 在领导者选举期间向其他节点请求投票
// - Leader: 处理客户端请求并将日志条目复制到跟随者
const (
	Follower NodeState = iota
	Candidate
	Leader
)

// String 返回 NodeState 的人类可读字符串表示。
// 这对于日志记录和调试目的很有用。
func (s NodeState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	default:
		return "Unknown"
	}
}

// NodeID 是 Raft 集群中每个节点的唯一标识符。
// 用于在通信过程中区分不同的节点。
type NodeID string

// LogEntry 表示 Raft 日志中的单个条目。
// 每个条目包含要应用到状态机的命令，
// 以及有关创建时间的元数据。
type LogEntry struct {
	Index   uint64 `json:"index"`   // 该条目在日志中的顺序索引
	Term    uint64 `json:"term"`    // 创建该条目时的 Raft 任期
	Command any    `json:"command"` // 要应用的实际命令/数据
}

// RequestVoteArgs 包含 RequestVote RPC 的参数。
// 此 RPC 由候选者在领导者选举期间调用以收集选票。
type RequestVoteArgs struct {
	Term         uint64 `json:"term"`           // 候选者的当前任期
	CandidateID  NodeID `json:"candidate_id"`  // 请求投票的候选者
	LastLogIndex uint64 `json:"last_log_index"` // 候选者最后日志条目的索引
	LastLogTerm  uint64 `json:"last_log_term"`  // 候选者最后日志条目的任期
}

// RequestVoteReply 包含对 RequestVote RPC 的响应。
// 回复指示是否授予投票以及响应者的当前任期。
type RequestVoteReply struct {
	Term        uint64 `json:"term"`         // 响应者的当前任期（供候选者更新自己）
	VoteGranted bool   `json:"vote_granted"` // 如果候选者收到投票则为真
}

// AppendEntriesArgs 包含 AppendEntries RPC 的参数。
// 此 RPC 用于心跳（空条目）和日志复制。
type AppendEntriesArgs struct {
	Term         uint64     `json:"term"`           // 领导者的当前任期
	LeaderID     NodeID     `json:"leader_id"`     // 领导者 ID（供跟随者重定向客户端）
	PrevLogIndex uint64     `json:"prev_log_index"` // 紧接新条目之前的日志条目索引
	PrevLogTerm  uint64     `json:"prev_log_term"`  // prevLogIndex 条目的任期
	Entries      []LogEntry `json:"entries"`        // 要存储的日志条目（心跳时为空）
	LeaderCommit uint64     `json:"leader_commit"`  // 领导者的提交索引
}

// AppendEntriesReply 包含对 AppendEntries RPC 的响应。
// 回复指示成功/失败和响应者的当前任期。
type AppendEntriesReply struct {
	Term    uint64 `json:"term"`    // 响应者的当前任期（供领导者更新自己）
	Success bool   `json:"success"` // 如果跟随者包含匹配 prevLogIndex 和 prevLogTerm 的条目则为真
}

// Config 保存 Raft 节点的配置参数。
// 包括网络设置、超时和操作参数。
type Config struct {
	NodeID           NodeID        `json:"node_id"`           // 此节点的唯一标识符
	Peers            []NodeID      `json:"peers"`             // 集群中其他节点的列表
	ElectionTimeout  time.Duration `json:"election_timeout"`  // 领导者选举超时
	HeartbeatTimeout time.Duration `json:"heartbeat_timeout"` // 领导者心跳间隔
	DataDir          string        `json:"data_dir"`          // 持久化存储目录
	ListenAddr       string        `json:"listen_addr"`       // HTTP 请求监听地址
}

// Command 表示将通过 Raft 日志复制的客户端命令。
// 命令是修改键值存储状态的操作。
type Command struct {
	Type  string `json:"type"`  // 操作类型（PUT、DELETE 等）
	Key   string `json:"key"`   // 要操作的键
	Value any    `json:"value"` // PUT 操作的值（DELETE 操作可选）
}

// ToJSON 将 Command 序列化为 JSON 字节。
// 在 Raft 日志中存储命令时使用。
func (c *Command) ToJSON() ([]byte, error) {
	return json.Marshal(c)
}

// CommandFromJSON 将 JSON 字节反序列化为 Command。
// 从 Raft 日志读取命令时使用。
func CommandFromJSON(data []byte) (*Command, error) {
	var cmd Command
	err := json.Unmarshal(data, &cmd)
	return &cmd, err
}