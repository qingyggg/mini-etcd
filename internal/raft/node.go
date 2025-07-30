// Package raft 实现了 Raft 共识算法。
// 这是分布式共识系统的核心，确保
// 集群中的所有节点对相同的操作序列达成一致。
package raft

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"mini-etcd/pkg/types"
)

// Node 表示 Raft 集群中的单个节点。
// 它维护 Raft 算法所需的所有状态，包括
// 当前任期、日志条目和领导信息。
type Node struct {
	// 用于保护节点状态并发访问的互斥锁
	mu sync.RWMutex
	
	// 节点标识和集群成员身份
	id    types.NodeID   // 此节点的唯一标识符
	peers []types.NodeID // 集群中所有其他节点的列表

	// 持久化状态（在响应 RPC 之前必须持久化）
	state       types.NodeState   // 当前节点状态（Follower/Candidate/Leader）
	currentTerm uint64            // 服务器见过的最新任期（单调递增）
	votedFor    *types.NodeID     // 当前任期内收到投票的候选者 ID（无则为 null）
	log         []types.LogEntry  // 日志条目；每个条目包含状态机命令

	// 易失状态（重启时丢失）
	commitIndex uint64 // 已知已提交的最高日志条目索引
	lastApplied uint64 // 已应用到状态机的最高日志条目索引

	// 领导者上的易失状态（选举后重新初始化）
	nextIndex  map[types.NodeID]uint64 // 对于每个服务器，要发送的下一个日志条目的索引
	matchIndex map[types.NodeID]uint64 // 对于每个服务器，已知已复制的最高日志条目索引

	// Raft 算法定时器
	electionTimer  *time.Timer // 触发领导者选举的定时器
	heartbeatTimer *time.Timer // 发送心跳的定时器（仅领导者）

	// 配置和通信
	config *types.Config // 节点配置参数

	// 通信通道
	applyCh chan types.LogEntry // 向状态机发送已提交条目的通道
	stopCh  chan struct{}       // 优雅停止节点的通道

	// 与其他节点通信的网络传输
	transport Transport
}

// Transport 定义了 Raft 节点之间网络通信的接口。
// 这种抽象允许不同的传输实现（HTTP、gRPC 等）
type Transport interface {
	// SendRequestVote 向目标节点发送 RequestVote RPC。
	// 在领导者选举期间用于向其他节点请求投票。
	SendRequestVote(target types.NodeID, args *types.RequestVoteArgs) (*types.RequestVoteReply, error)
	
	// SendAppendEntries 向目标节点发送 AppendEntries RPC。
	// 用于日志复制和作为心跳来维持领导权。
	SendAppendEntries(target types.NodeID, args *types.AppendEntriesArgs) (*types.AppendEntriesReply, error)
}

// NewNode 使用给定的配置和传输创建一个新的 Raft 节点。
// 节点以 Follower 状态开始并初始化所有必要的数据结构。
func NewNode(config *types.Config, transport Transport) *Node {
	n := &Node{
		id:        config.NodeID,
		peers:     config.Peers,
		state:     types.Follower,
		log:       make([]types.LogEntry, 0),
		nextIndex: make(map[types.NodeID]uint64),
		matchIndex: make(map[types.NodeID]uint64),
		config:    config,
		applyCh:   make(chan types.LogEntry, 100),
		stopCh:    make(chan struct{}),
		transport: transport,
	}

	// 尝试从持久化存储恢复状态
	if err := n.restore(); err != nil {
		log.Printf("节点 %s 恢复状态失败: %v", n.id, err)
	}

	n.resetElectionTimer()
	return n
}

// Start 在单独的 goroutine 中开始节点的主事件循环。
// 节点将立即开始参与 Raft 协议。
func (n *Node) Start() {
	go n.run()
}

// Stop 通过关闭停止通道优雅地关闭节点。
// 这将导致主事件循环退出。
func (n *Node) Stop() {
	close(n.stopCh)
}

// run 是 Raft 节点的主事件循环。
// 它根据当前节点状态处理选举和心跳的超时。
func (n *Node) run() {
	for {
		select {
		case <-n.stopCh:
			return
		case <-n.electionTimer.C:
			n.mu.Lock()
			if n.state != types.Leader {
				n.startElection()
			}
			n.mu.Unlock()
		case <-n.heartbeatTimer.C:
			n.mu.Lock()
			if n.state == types.Leader {
				n.sendHeartbeats()
			}
			n.mu.Unlock()
		}
	}
}

// startElection 发起一次新的领导者选举。
// 节点转换为 Candidate 状态并向所有对等节点请求投票。
func (n *Node) startElection() {
	// 按照 Raft 算法转换为候选者状态
	n.state = types.Candidate
	n.currentTerm++         // 增加当前任期
	n.votedFor = &n.id      // 给自己投票
	
	// 持久化状态变更
	if err := n.persist(); err != nil {
		log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
	}
	
	n.resetElectionTimer()  // 重置选举定时器

	log.Printf("节点 %s 开始任期 %d 的选举", n.id, n.currentTerm)

	// 获取我们最后日志条目的信息以用于投票请求
	lastLogIndex := uint64(len(n.log))
	lastLogTerm := uint64(0)
	if lastLogIndex > 0 {
		lastLogTerm = n.log[lastLogIndex-1].Term
	}

	args := &types.RequestVoteArgs{
		Term:         n.currentTerm,
		CandidateID:  n.id,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}

	// 从一票开始（我们自己的票）
	votes := 1
	
	// 并行向所有对等节点发送 RequestVote RPC
	for _, peer := range n.peers {
		go func(peer types.NodeID) {
			reply, err := n.transport.SendRequestVote(peer, args)
			if err != nil {
				return
			}

			n.mu.Lock()
			defer n.mu.Unlock()

			// 如果我们发现更高的任期，立即退下
			if reply.Term > n.currentTerm {
				n.currentTerm = reply.Term
				n.state = types.Follower
				n.votedFor = nil
				// 持久化状态变更
				if err := n.persist(); err != nil {
					log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
				}
				n.resetElectionTimer()
				return
			}

			// 统计选票并检查是否获得大多数
			if n.state == types.Candidate && reply.VoteGranted {
				votes++
				// 需要总节点数的大多数（包括我们自己）
				if votes > (len(n.peers)+1)/2 {
					n.becomeLeader()
				}
			}
		}(peer)
	}
}

// becomeLeader 将节点转换为 Leader 状态。
// 它初始化领导者特定状态并开始发送心跳。
func (n *Node) becomeLeader() {
	n.state = types.Leader
	log.Printf("节点 %s 成为任期 %d 的领导者", n.id, n.currentTerm)

	// 为每个跟随者初始化领导者状态
	for _, peer := range n.peers {
		// 乐观地假设跟随者都是最新的
		n.nextIndex[peer] = uint64(len(n.log)) + 1
		// 还没有确认复制的条目
		n.matchIndex[peer] = 0
	}

	// 立即开始发送心跳来建立权威
	n.resetHeartbeatTimer()
	n.sendHeartbeats()
}

// sendHeartbeats 向所有跟随者发送 AppendEntries RPC。
// 这维持了领导者的权威并防止新的选举。
func (n *Node) sendHeartbeats() {
	for _, peer := range n.peers {
		go func(peer types.NodeID) {
			n.sendAppendEntries(peer)
		}(peer)
	}
	n.resetHeartbeatTimer()
}

// sendAppendEntries 向特定对等节点发送 AppendEntries RPC。
// 这处理心跳和实际日志复制。
func (n *Node) sendAppendEntries(peer types.NodeID) {
	n.mu.RLock()
	if n.state != types.Leader {
		n.mu.RUnlock()
		return
	}

	nextIndex := n.nextIndex[peer]
	prevLogIndex := nextIndex - 1
	prevLogTerm := uint64(0)

	if prevLogIndex > 0 && prevLogIndex <= uint64(len(n.log)) {
		prevLogTerm = n.log[prevLogIndex-1].Term
	}

	entries := make([]types.LogEntry, 0)
	if nextIndex <= uint64(len(n.log)) {
		entries = n.log[nextIndex-1:]
	}

	args := &types.AppendEntriesArgs{
		Term:         n.currentTerm,
		LeaderID:     n.id,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: n.commitIndex,
	}
	n.mu.RUnlock()

	reply, err := n.transport.SendAppendEntries(peer, args)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()

	if reply.Term > n.currentTerm {
		n.currentTerm = reply.Term
		n.state = types.Follower
		n.votedFor = nil
		// 持久化状态变更
		if err := n.persist(); err != nil {
			log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		}
		n.resetElectionTimer()
		return
	}

	if n.state == types.Leader {
		if reply.Success {
			n.nextIndex[peer] = nextIndex + uint64(len(entries))
			n.matchIndex[peer] = n.nextIndex[peer] - 1
			
			// 检查是否可以提交更多日志条目
			n.updateCommitIndex()
		} else {
			n.nextIndex[peer] = max(1, n.nextIndex[peer]-1)
		}
	}
}

// updateCommitIndex 检查是否可以提交更多日志条目。
// 只有当大多数服务器都已复制了某个索引时，该索引才能被提交。
func (n *Node) updateCommitIndex() {
	if n.state != types.Leader {
		return
	}

	// 从当前提交索引开始检查
	for i := n.commitIndex + 1; i <= uint64(len(n.log)); i++ {
		if n.log[i-1].Term != n.currentTerm {
			continue // 只能提交当前任期的日志条目
		}

		// 计算有多少个服务器已经复制了这个索引
		count := 1 // 包括领导者自己
		for _, peer := range n.peers {
			if n.matchIndex[peer] >= i {
				count++
			}
		}

		// 如果大多数服务器都有这个条目，则可以提交
		if count > (len(n.peers)+1)/2 {
			n.commitIndex = i
			n.applyCommittedEntries()
		} else {
			break // 如果这个索引不能提交，更高的索引也不能提交
		}
	}
}

// resetElectionTimer 使用随机持续时间重置选举超时。
// 随机化有助于防止选举期间的分裂投票。
func (n *Node) resetElectionTimer() {
	if n.electionTimer != nil {
		n.electionTimer.Stop()
	}
	timeout := n.config.ElectionTimeout + time.Duration(rand.Int63n(int64(n.config.ElectionTimeout)))
	n.electionTimer = time.NewTimer(timeout)
}

// resetHeartbeatTimer 为领导者重置心跳定时器。
// 领导者发送定期心跳来维持其权威。
func (n *Node) resetHeartbeatTimer() {
	if n.heartbeatTimer != nil {
		n.heartbeatTimer.Stop()
	}
	n.heartbeatTimer = time.NewTimer(n.config.HeartbeatTimeout)
}

// RequestVote 处理来自候选者的传入 RequestVote RPC。
// 它根据 Raft 的投票规则决定是否授予投票。
func (n *Node) RequestVote(args *types.RequestVoteArgs) *types.RequestVoteReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := &types.RequestVoteReply{
		Term:        n.currentTerm,
		VoteGranted: false,
	}

	// 如果候选者的任期更高，更新我们的任期并成为跟随者
	if args.Term > n.currentTerm {
		n.currentTerm = args.Term
		n.state = types.Follower
		n.votedFor = nil
		// 持久化状态变更
		if err := n.persist(); err != nil {
			log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		}
	}

	// 拒绝来自较低任期的投票
	if args.Term < n.currentTerm {
		return reply
	}

	// 检查候选者的日志是否至少与我们的一样新
	lastLogIndex := uint64(len(n.log))
	lastLogTerm := uint64(0)
	if lastLogIndex > 0 {
		lastLogTerm = n.log[lastLogIndex-1].Term
	}

	// 日志是最新的如果：更高的任期 或 相同任期但更高/相等的索引
	logUpToDate := args.LastLogTerm > lastLogTerm ||
		(args.LastLogTerm == lastLogTerm && args.LastLogIndex >= lastLogIndex)

	// 如果我们还没有投票（或投票给同一候选者）且日志是最新的，则授予投票
	if (n.votedFor == nil || *n.votedFor == args.CandidateID) && logUpToDate {
		n.votedFor = &args.CandidateID
		reply.VoteGranted = true
		// 持久化投票决定
		if err := n.persist(); err != nil {
			log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		}
		// 重置选举定时器，因为我们正在确认一个有效的候选者
		n.resetElectionTimer()
	}

	return reply
}

// AppendEntries 处理来自领导者的传入 AppendEntries RPC。
// 它处理心跳和日志复制请求。
func (n *Node) AppendEntries(args *types.AppendEntriesArgs) *types.AppendEntriesReply {
	n.mu.Lock()
	defer n.mu.Unlock()

	reply := &types.AppendEntriesReply{
		Term:    n.currentTerm,
		Success: false,
	}

	if args.Term > n.currentTerm {
		n.currentTerm = args.Term
		n.state = types.Follower
		n.votedFor = nil
		// 持久化状态变更
		if err := n.persist(); err != nil {
			log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		}
	}

	if args.Term < n.currentTerm {
		return reply
	}

	n.resetElectionTimer()
	n.state = types.Follower

	if args.PrevLogIndex > 0 {
		if args.PrevLogIndex > uint64(len(n.log)) ||
			n.log[args.PrevLogIndex-1].Term != args.PrevLogTerm {
			return reply
		}
	}

	if len(args.Entries) > 0 {
		n.log = n.log[:args.PrevLogIndex]
		n.log = append(n.log, args.Entries...)
		// 持久化日志变更
		if err := n.persist(); err != nil {
			log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		}
	}

	if args.LeaderCommit > n.commitIndex {
		n.commitIndex = min(args.LeaderCommit, uint64(len(n.log)))
		n.applyCommittedEntries()
	}

	reply.Success = true
	return reply
}

// applyCommittedEntries 将新提交的日志条目发送到状态机。
// 这是命令一旦被提交就实际执行的方式。
func (n *Node) applyCommittedEntries() {
	for n.lastApplied < n.commitIndex {
		n.lastApplied++
		entry := n.log[n.lastApplied-1]
		select {
		case n.applyCh <- entry:
		default:
		}
	}
}

// Submit 接受来自客户端的新命令并将其添加到日志中。
// 只有领导者可以处理客户端命令；跟随者将拒绝它们。
func (n *Node) Submit(command any) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.state != types.Leader {
		return fmt.Errorf("not leader")
	}

	entry := types.LogEntry{
		Index:   uint64(len(n.log)) + 1,
		Term:    n.currentTerm,
		Command: command,
	}

	n.log = append(n.log, entry)
	
	// 持久化新的日志条目
	if err := n.persist(); err != nil {
		log.Printf("节点 %s 持久化状态失败: %v", n.id, err)
		return err
	}
	
	return nil
}

// GetApplyCh 返回一个只读通道用于接收已提交的日志条目。
// 状态机应该监听此通道来应用已提交的命令。
func (n *Node) GetApplyCh() <-chan types.LogEntry {
	return n.applyCh
}

// max 返回两个 uint64 值中的较大者。
// 在各种 Raft 算法计算中使用的辅助函数。
func max(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// min 返回两个 uint64 值中的较小者。
// 在各种 Raft 算法计算中使用的辅助函数。
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}