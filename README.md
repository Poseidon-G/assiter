# Assiter

> **Code Knowledge Graph for AI Agents** — built in Go

Parses a code repository into a semantic Neo4j graph, then exposes it to an OpenAI-compatible AI agent via REST API and CLI. The agent uses structured graph context to clarify requirements, explain impact, and avoid breaking changes.

## Architecture

```
Code Repo
    │
    ▼
Tree-sitter Parser  (Go, Python, TypeScript, Java, Rust, C++)
    │
    ▼
Normalizer          ⭐ AST → Unified UModel
    │
    ▼
Neo4j Graph         Nodes: File, Package, Function, Method, Struct, Interface, Variable, Import
    │               Edges: CONTAINS, CALLS, IMPLEMENTS, IMPORTS, DEPENDS_ON, DEFINES
    ▼
AI Agent            OpenAI-compatible (GPT-4o, Claude, etc.)
    │
    ▼
REST API + CLI
```

## Quick Start

### Prerequisites
- Go 1.25+
- Neo4j 5.x running locally (default: `bolt://localhost:7687`)
- OpenAI API key (or compatible endpoint)

### Configure

Copy and edit `assiter.yaml`:
```yaml
neo4j:
  uri: "bolt://localhost:7687"
  username: "neo4j"
  password: "your-password"

openai:
  api_key: "sk-..."
  model: "gpt-4o"
```

Or use environment variables:
```bash
export ASSITER_NEO4J__PASSWORD=your-password
export ASSITER_OPENAI__API_KEY=sk-...
```

### CLI

```bash
# Build
go build -o assiter ./cmd/cli

# Ingest a repository
./assiter ingest /path/to/your/repo

# Ask the AI about the codebase
./assiter query "Which functions call the authentication handler?"
./assiter query "What would break if I change the User struct?" --symbol User

# Explore the graph
./assiter graph stats
./assiter graph search UserService

# Start REST API server
./assiter serve
```

### REST API

```bash
# Start server binary directly
go run ./cmd/server

# Ingest a repo
curl -X POST http://localhost:8080/ingest \
  -H 'Content-Type: application/json' \
  -d '{"dir": "/path/to/repo"}'

# Search the graph
curl "http://localhost:8080/graph/search?name=UserService"

# Get a node and its neighbors
curl "http://localhost:8080/graph/node/<node-id>"

# Graph statistics
curl http://localhost:8080/graph/stats

# Ask the AI agent
curl -X POST http://localhost:8080/agent/query \
  -H 'Content-Type: application/json' \
  -d '{"question": "What does the auth package do?", "symbol": "Auth"}'

# Health check
curl http://localhost:8080/health
```

## Project Structure

```
assiter/
├── cmd/
│   ├── cli/main.go          # CLI entrypoint (cobra)
│   └── server/main.go       # REST server entrypoint
├── internal/
│   ├── parser/              # Multi-language AST parsers
│   │   ├── parser.go        # Language dispatch + file walker
│   │   ├── golang.go        # Go (go/ast stdlib)
│   │   ├── python.go        # Python (line-based)
│   │   ├── typescript.go    # TypeScript/TSX (line-based)
│   │   ├── java.go          # Java (line-based)
│   │   ├── rust.go          # Rust (line-based)
│   │   └── cpp.go           # C/C++ (line-based)
│   ├── normalizer/          # Raw AST → UModel graph
│   ├── umodel/              # Unified model types (Node, Edge, Graph)
│   ├── graph/               # Neo4j client, schema, queries
│   ├── ingestion/           # End-to-end pipeline
│   ├── agent/               # AI agent + OpenAI integration
│   └── api/                 # Gin REST handlers
├── pkg/config/              # YAML + env config (Viper)
├── assiter.yaml             # Default config
└── go.mod
```

## UModel Node Types

| Type      | Description |
|-----------|-------------|
| File      | Source file with path and checksum |
| Package   | Package/module/namespace |
| Function  | Top-level function |
| Method    | Method on a type/class |
| Struct    | Struct, class, or enum |
| Interface | Interface or trait |
| Variable  | Package-level variable/constant |
| Import    | Import/use/include statement |

## Supported Languages

| Language   | Parser strategy |
|------------|----------------|
| Go         | `go/ast` standard library (full AST) |
| Python     | Line-based extractor |
| TypeScript | Line-based extractor |
| Java       | Line-based extractor |
| Rust       | Line-based extractor |
| C/C++      | Line-based extractor |
# assiter
