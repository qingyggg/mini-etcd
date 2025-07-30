#!/bin/bash

# Mini-etcd 集群示例
# 此脚本启动一个 3 节点集群用于测试和演示。
# 它演示如何配置和运行一个分布式 etcd 类系统。

set -e

echo "构建 mini-etcd..."
go build -o mini-etcd cmd/main.go

echo "启动 3 节点集群..."

# 清理任何现有的数据目录以便重新开始
rm -rf ./data1 ./data2 ./data3

# 启动 node1 - 集群中的第一个节点
./mini-etcd \
  -id=node1 \
  -peers=node2,node3 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8081 \
  -data-dir=./data1 &
NODE1_PID=$!

# 启动 node2 - 集群中的第二个节点
./mini-etcd \
  -id=node2 \
  -peers=node1,node3 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8082 \
  -data-dir=./data2 &
NODE2_PID=$!

# 启动 node3 - 集群中的第三个节点
./mini-etcd \
  -id=node3 \
  -peers=node1,node2 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8083 \
  -data-dir=./data3 &
NODE3_PID=$!

echo "集群已启动，进程 ID: $NODE1_PID $NODE2_PID $NODE3_PID"
echo "访问节点:"
echo "  节点1: http://localhost:8081"
echo "  节点2: http://localhost:8082"
echo "  节点3: http://localhost:8083"
echo ""
echo "尝试一些命令:"
echo "  curl -X PUT http://localhost:8081/v2/keys/foo -d value=bar"
echo "  curl -X GET http://localhost:8081/v2/keys/foo"
echo "  curl -X DELETE http://localhost:8081/v2/keys/foo"
echo ""
echo "按 Ctrl+C 停止集群"

# 干净地关闭所有节点的函数
cleanup() {
    echo "正在停止集群..."
    # 优雅地杀死所有节点进程
    kill $NODE1_PID $NODE2_PID $NODE3_PID 2>/dev/null || true
    wait
    echo "集群已停止"
}

trap cleanup EXIT
wait