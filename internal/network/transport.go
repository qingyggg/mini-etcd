// Package network 为 Raft 节点通信提供 HTTP 传输层。
// 这使用 HTTP/JSON 实现 Transport 接口用于节点间消息传递。
package network

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mini-etcd/pkg/types"
)

// HTTPTransport 使用 HTTP 实现 Transport 接口。
// 它维持节点 ID 到其 HTTP 地址的映射，并使用
// 共享的 HTTP 客户端处理所有出站请求。
type HTTPTransport struct {
	peerAddrs map[types.NodeID]string // 将节点 ID 映射到其 HTTP 地址
	client    *http.Client            // 用于发送请求的 HTTP 客户端
}

// NewHTTPTransport 使用给定的对等节点地址创建一个新的 HTTP 传输。
// 传输配置了适合 Raft 通信的合理超时。
func NewHTTPTransport(peerAddrs map[types.NodeID]string) *HTTPTransport {
	return &HTTPTransport{
		peerAddrs: peerAddrs,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// SendRequestVote 通过 HTTP POST 向目标节点发送 RequestVote RPC。
// 在领导者选举期间用于向其他节点请求投票。
func (t *HTTPTransport) SendRequestVote(target types.NodeID, args *types.RequestVoteArgs) (*types.RequestVoteReply, error) {
	addr, exists := t.peerAddrs[target]
	if !exists {
		return nil, fmt.Errorf("unknown peer: %s", target)
	}

	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Post(
		fmt.Sprintf("http://%s/raft/requestvote", addr),
		"application/json",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	var reply types.RequestVoteReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}

	return &reply, nil
}

// SendAppendEntries 通过 HTTP POST 向目标节点发送 AppendEntries RPC。
// 用于领导者和跟随者之间的心跳和日志复制。
func (t *HTTPTransport) SendAppendEntries(target types.NodeID, args *types.AppendEntriesArgs) (*types.AppendEntriesReply, error) {
	addr, exists := t.peerAddrs[target]
	if !exists {
		return nil, fmt.Errorf("unknown peer: %s", target)
	}

	data, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Post(
		fmt.Sprintf("http://%s/raft/appendentries", addr),
		"application/json",
		bytes.NewBuffer(data),
	)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	var reply types.AppendEntriesReply
	if err := json.NewDecoder(resp.Body).Decode(&reply); err != nil {
		return nil, err
	}

	return &reply, nil
}