// Package main 是 mini-etcd 应用程序的入口点。
// 它设置并启动所有必要的组件：Raft 节点、键值存储、
// 网络传输和 HTTP API 服务器。
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mini-etcd/internal/api"
	"mini-etcd/internal/network"
	"mini-etcd/internal/raft"
	"mini-etcd/internal/store"
	"mini-etcd/pkg/types"
)

// main 是 mini-etcd 应用程序的入口点。
// 它解析命令行参数、设置所有组件并运行服务器。
func main() {
	// 解析配置的命令行标志
	var (
		nodeID     = flag.String("id", "node1", "Node ID")
		peers      = flag.String("peers", "", "Comma-separated list of peer node IDs")
		peerAddrs  = flag.String("peer-addrs", "", "Comma-separated list of peer addresses (format: id1=addr1,id2=addr2)")
		listenAddr = flag.String("listen", ":8080", "Listen address")
		dataDir    = flag.String("data-dir", "./data", "Data directory")
	)
	flag.Parse()

	// 从命令行参数解析对等节点列表
	peerList := []types.NodeID{}
	if *peers != "" {
		for _, peer := range strings.Split(*peers, ",") {
			if peer != *nodeID {
				peerList = append(peerList, types.NodeID(strings.TrimSpace(peer)))
			}
		}
	}

	// 从命令行参数解析对等节点地址映射
	peerAddrMap := make(map[types.NodeID]string)
	if *peerAddrs != "" {
		for _, pair := range strings.Split(*peerAddrs, ",") {
			parts := strings.Split(pair, "=")
			if len(parts) == 2 {
				peerAddrMap[types.NodeID(parts[0])] = parts[1]
			}
		}
	}

	// 为此 Raft 节点创建配置
	config := &types.Config{
		NodeID:           types.NodeID(*nodeID),
		Peers:            peerList,
		ElectionTimeout:  150 * time.Millisecond, // 在 150-300ms 之间随机化
		HeartbeatTimeout: 50 * time.Millisecond,  // 领导者每 50ms 发送一次心跳
		DataDir:          *dataDir,
		ListenAddr:       *listenAddr,
	}

	// 创建键值存储（状态机）
	kvStore := store.NewStore()

	// 创建用于节点间通信的网络传输
	transport := network.NewHTTPTransport(peerAddrMap)

	// 创建 Raft 共识节点
	node := raft.NewNode(config, transport)

	// 启动一个 goroutine 将已提交的 Raft 条目应用到键值存储
	go func() {
		for entry := range node.GetApplyCh() {
			if err := kvStore.Apply(entry); err != nil {
				log.Printf("Failed to apply entry: %v", err)
			}
		}
	}()

	// 启动 Raft 节点
	node.Start()

	// 创建并启动 HTTP API 服务器
	server := api.NewServer(kvStore, node, *listenAddr)

	go func() {
		if err := server.Start(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// 记录启动信息
	log.Printf("Mini-etcd 节点 %s 在 %s 上启动", *nodeID, *listenAddr)
	log.Printf("对等节点: %v", peerList)
	log.Printf("数据目录: %s", *dataDir)

	// 等待关闭信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	// 优雅关闭
	log.Println("正在关闭...")
	node.Stop()
}