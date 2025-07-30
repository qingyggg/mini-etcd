# Mini-etcd

A simplified implementation of etcd using the Raft consensus algorithm.

## Features

- Raft consensus algorithm implementation
- Key-value store with TTL support
- etcd-compatible HTTP API (v2)
- Cluster management and leader election
- Basic persistence and logging
- Health checks and statistics

## Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│     Node 1      │    │     Node 2      │    │     Node 3      │
├─────────────────┤    ├─────────────────┤    ├─────────────────┤
│  API Server     │    │  API Server     │    │  API Server     │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │ KV Store  │  │    │  │ KV Store  │  │    │  │ KV Store  │  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
│  ┌───────────┐  │    │  ┌───────────┐  │    │  ┌───────────┐  │
│  │Raft Engine│  │    │  │Raft Engine│  │    │  │Raft Engine│  │
│  └───────────┘  │    │  └───────────┘  │    │  └───────────┘  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                       │                       │
         └───────────────────────┼───────────────────────┘
                           Network Transport
```

## Quick Start

### Build

```bash
go build -o mini-etcd cmd/main.go
```

### Start a Single Node

```bash
./mini-etcd -id=node1 -listen=:8080 -data-dir=./data1
```

### Start a 3-Node Cluster

Use the provided cluster script:

```bash
./examples/cluster.sh
```

Or manually:

```bash
# Terminal 1 - Node 1
./mini-etcd -id=node1 -peers=node2,node3 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8081 -data-dir=./data1

# Terminal 2 - Node 2  
./mini-etcd -id=node2 -peers=node1,node3 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8082 -data-dir=./data2

# Terminal 3 - Node 3
./mini-etcd -id=node3 -peers=node1,node2 \
  -peer-addrs=node1=localhost:8081,node2=localhost:8082,node3=localhost:8083 \
  -listen=:8083 -data-dir=./data3
```

## API Usage

### Set a key-value pair

```bash
curl -X PUT http://localhost:8081/v2/keys/foo -d value=bar
```

### Get a value

```bash
curl -X GET http://localhost:8081/v2/keys/foo
```

### Set with TTL (expires in 60 seconds)

```bash
curl -X PUT http://localhost:8081/v2/keys/temp -d value=test -d ttl=60
```

### Delete a key

```bash
curl -X DELETE http://localhost:8081/v2/keys/foo
```

### Health check

```bash
curl -X GET http://localhost:8081/health
```

### Statistics

```bash
curl -X GET http://localhost:8081/stats
```

## Client Example

Use the provided Go client:

```bash
# Set a value
go run examples/client.go set mykey myvalue

# Get a value
go run examples/client.go get mykey

# Delete a key
go run examples/client.go del mykey
```

## Testing

Run the tests:

```bash
go test ./tests/...
```

## Command Line Options

- `-id`: Node ID (default: "node1")
- `-peers`: Comma-separated list of peer node IDs
- `-peer-addrs`: Comma-separated list of peer addresses (format: id1=addr1,id2=addr2)
- `-listen`: Listen address (default: ":8080")
- `-data-dir`: Data directory for persistence (default: "./data")

## Implementation Details

### Raft Algorithm

- **Leader Election**: Nodes elect a leader using randomized timeouts
- **Log Replication**: Leader replicates log entries to followers
- **Safety**: Ensures strong consistency across the cluster
- **Fault Tolerance**: Tolerates up to (n-1)/2 node failures

### Key-Value Store

- In-memory storage with optional persistence
- TTL support for automatic key expiration
- Thread-safe operations with read-write locks

### Network Transport

- HTTP-based communication between nodes
- JSON serialization for Raft messages
- Configurable timeouts and retries

## Features Implemented

✅ **Raft Consensus Algorithm**
- Leader election with randomized timeouts
- Log replication with consistency guarantees
- Proper state persistence (currentTerm, votedFor, log entries)
- Commit index management
- Network partition tolerance

✅ **Key-Value Store**
- Thread-safe operations
- TTL support with automatic cleanup
- JSON-based command serialization

✅ **Network Communication**
- HTTP/JSON transport layer
- Request/response handling for Raft RPCs

✅ **API Compatibility**
- etcd v2 compatible endpoints
- Health checks and statistics

## Differences from Real etcd

This is a simplified implementation for educational purposes. Key differences:

1. **Performance**: Not optimized for production use
2. **Persistence**: Basic JSON file persistence vs. etcd's WAL
3. **API**: Limited subset of etcd v2 API
4. **Features**: Missing many production features (auth, SSL, etc.)
5. **Scalability**: Not tested at scale
6. **Consistency**: Simplified commit confirmation (uses timeouts instead of proper acknowledgments)

## License

MIT License