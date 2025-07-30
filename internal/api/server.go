// Package api 提供暴露 etcd 兼容端点的 HTTP API 服务器。
// 这一层处理客户端请求并将它们转换为 Raft 操作。
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"mini-etcd/internal/raft"
	"mini-etcd/internal/store"
	"mini-etcd/pkg/types"
)

// Server 表示处理客户端请求的 HTTP API 服务器。
// 它在 HTTP 客户端和 Raft 共识层之间搭起桥梁。
type Server struct {
	store *store.Store // 键值存储（状态机）
	node  *raft.Node   // Raft 共识节点
	addr  string       // 监听地址
}

// EtcdResponse 表示标准的 etcd API 响应格式。
// 这保持与 etcd v2 API 客户端的兼容性。
type EtcdResponse struct {
	Action string     `json:"action"`           // 执行的操作（get、set、delete 等）
	Node   *EtcdNode  `json:"node,omitempty"`   // 成功操作的节点信息
	Error  *EtcdError `json:"error,omitempty"`  // 失败操作的错误信息
}

// EtcdNode 表示 etcd 键空间中的节点。
// 此结构在 API 响应中返回以提供键元数据。
type EtcdNode struct {
	Key           string `json:"key"`                   // 键路径
	Value         string `json:"value,omitempty"`       // 键的值
	CreatedIndex  uint64 `json:"createdIndex"`          // 创建键时的索引
	ModifiedIndex uint64 `json:"modifiedIndex"`        // 最后修改键时的索引
	TTL           int64  `json:"ttl,omitempty"`         // 生存时间（秒）
}

// EtcdError 表示 etcd API 格式的错误。
// 这为客户端提供结构化的错误信息。
type EtcdError struct {
	ErrorCode int    `json:"errorCode"`        // 数字错误代码
	Message   string `json:"message"`          // 人类可读的错误消息
	Cause     string `json:"cause,omitempty"`  // 附加错误详情
}

// NewServer 使用给定的存储、Raft 节点和地址创建一个新的 API 服务器。
// 服务器将处理 HTTP 请求并与 Raft 层协调。
func NewServer(store *store.Store, node *raft.Node, addr string) *Server {
	return &Server{
		store: store,
		node:  node,
		addr:  addr,
	}
}

// Start 在配置的地址上开始处理 HTTP 请求。
// 它设置所有 API 路由并启动 HTTP 服务器。
func (s *Server) Start() error {
	r := mux.NewRouter()

	r.HandleFunc("/v2/keys/{key:.*}", s.handleKey).Methods("GET", "PUT", "DELETE")
	r.HandleFunc("/raft/requestvote", s.handleRequestVote).Methods("POST")
	r.HandleFunc("/raft/appendentries", s.handleAppendEntries).Methods("POST")
	r.HandleFunc("/health", s.handleHealth).Methods("GET")
	r.HandleFunc("/stats", s.handleStats).Methods("GET")

	log.Printf("Starting API server on %s", s.addr)
	return http.ListenAndServe(s.addr, r)
}

// handleKey 根据 HTTP 方法将与键相关的请求路由到适当的处理程序。
// 这是 etcd 键操作（GET、PUT、DELETE）的主要入口点。
func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	key := vars["key"]

	switch r.Method {
	case "GET":
		s.handleGet(w, r, key)
	case "PUT":
		s.handlePut(w, r, key)
	case "DELETE":
		s.handleDelete(w, r, key)
	}
}

// handleGet 处理 GET 请求以检索键值。
// 它直接从本地存储读取而不经过 Raft。
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, key string) {
	value, exists := s.store.Get(key)
	if !exists {
		s.writeError(w, http.StatusNotFound, 100, "Key not found", "")
		return
	}

	response := &EtcdResponse{
		Action: "get",
		Node: &EtcdNode{
			Key:           key,
			Value:         value,
			CreatedIndex:  1,
			ModifiedIndex: 1,
		},
	}

	s.writeJSON(w, response)
}

// handlePut 处理 PUT 请求以设置键值。
// 它通过 Raft 提交操作以确保集群中的一致性。
func (s *Server) handlePut(w http.ResponseWriter, r *http.Request, key string) {
	if err := r.ParseForm(); err != nil {
		s.writeError(w, http.StatusBadRequest, 200, "Invalid form data", err.Error())
		return
	}

	value := r.FormValue("value")
	if value == "" {
		s.writeError(w, http.StatusBadRequest, 200, "Value is required", "")
		return
	}

	ttlStr := r.FormValue("ttl")
	var ttl int64
	if ttlStr != "" {
		var err error
		ttl, err = strconv.ParseInt(ttlStr, 10, 64)
		if err != nil {
			s.writeError(w, http.StatusBadRequest, 200, "Invalid TTL", err.Error())
			return
		}
	}

	req := store.KVRequest{
		Type:  "PUT",
		Key:   key,
		Value: value,
		TTL:   ttl,
	}

	data, err := json.Marshal(req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, 300, "Internal error", err.Error())
		return
	}

	if err := s.node.Submit(data); err != nil {
		s.writeError(w, http.StatusInternalServerError, 300, "Failed to submit", err.Error())
		return
	}

	// TODO: 这是简化实现，应该等待实际的提交确认而不是固定延迟
	time.Sleep(50 * time.Millisecond)

	response := &EtcdResponse{
		Action: "set",
		Node: &EtcdNode{
			Key:           key,
			Value:         value,
			CreatedIndex:  1,
			ModifiedIndex: 1,
			TTL:           ttl,
		},
	}

	s.writeJSON(w, response)
}

// handleDelete 处理 DELETE 请求以删除键。
// 它通过 Raft 提交操作以确保集群中的一致性。
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, key string) {
	if _, exists := s.store.Get(key); !exists {
		s.writeError(w, http.StatusNotFound, 100, "Key not found", "")
		return
	}

	req := store.KVRequest{
		Type: "DELETE",
		Key:  key,
	}

	data, err := json.Marshal(req)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, 300, "Internal error", err.Error())
		return
	}

	if err := s.node.Submit(data); err != nil {
		s.writeError(w, http.StatusInternalServerError, 300, "Failed to submit", err.Error())
		return
	}

	// TODO: 这是简化实现，应该等待实际的提交确认而不是固定延迟
	time.Sleep(50 * time.Millisecond)

	response := &EtcdResponse{
		Action: "delete",
		Node: &EtcdNode{
			Key:           key,
			CreatedIndex:  1,
			ModifiedIndex: 1,
		},
	}

	s.writeJSON(w, response)
}

// handleRequestVote 处理来自其他 Raft 节点的传入 RequestVote RPC。
// 这是 Raft 协议领导者选举机制的一部分。
func (s *Server) handleRequestVote(w http.ResponseWriter, r *http.Request) {
	var args types.RequestVoteArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reply := s.node.RequestVote(&args)
	s.writeJSON(w, reply)
}

// handleAppendEntries 处理来自领导者的传入 AppendEntries RPC。
// 这是 Raft 协议日志复制机制的一部分。
func (s *Server) handleAppendEntries(w http.ResponseWriter, r *http.Request) {
	var args types.AppendEntriesArgs
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reply := s.node.AppendEntries(&args)
	s.writeJSON(w, reply)
}

// handleHealth 提供一个简单的健康检查端点。
// 可以被负载均衡器和监控系统使用。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status": "ok",
		"time":   time.Now().Unix(),
	}
	s.writeJSON(w, health)
}

// handleStats 提供有关存储当前状态的统计信息。
// 这对于监控和调试目的很有用。
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]interface{}{
		"store_size": s.store.Size(),
		"keys":       s.store.List(),
	}
	s.writeJSON(w, stats)
}

// writeJSON 是写入 JSON 响应的辅助函数。
// 它设置适当的内容类型头并编码数据。
func (s *Server) writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// writeError 是以 etcd 格式写入错误响应的辅助函数。
// 它使用给定的详情创建正确格式化的错误响应。
func (s *Server) writeError(w http.ResponseWriter, status int, errorCode int, message, cause string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := &EtcdResponse{
		Error: &EtcdError{
			ErrorCode: errorCode,
			Message:   message,
			Cause:     cause,
		},
	}

	json.NewEncoder(w).Encode(response)
}