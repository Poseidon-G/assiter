# Assiter

> **Code Knowledge Graph for AI Agents** — built in Go

Parses a code repository into a semantic Neo4j graph, then exposes it to AI agents via REST API, CLI, and **MCP server**. Agents use structured graph context to clarify requirements, explain impact, and avoid breaking changes.

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
Neo4j Graph         Nodes: File, Package, Function, Method, Struct, Interface, Variable, Import, Symbol
    │               Edges: CONTAINS, CALLS, IMPLEMENTS, IMPORTS, DEPENDS_ON, DEFINES
    ▼
MCP Server ──────── GitHub Copilot, Claude, Cursor, any MCP client
REST API + CLI
```

## Quick Start

### Prerequisites
- Go 1.25+
- Neo4j 5.x running locally (default: `bolt://localhost:7687`)
- OpenAI API key (or compatible endpoint)

### Configure

```bash
cp assiter.example.yaml assiter.yaml
# edit assiter.yaml with your Neo4j password and LLM key
```

Environment variables also work (prefix `ASSITER_`, `__` for nesting):
```bash
export ASSITER_NEO4J__PASSWORD=your-password
export ASSITER_LLM__API_KEY=sk-...
```

### CLI

```bash
go build -o assiter ./cmd/cli

# Ingest a repository (first time)
./assiter ingest /path/to/repo

# Force re-ingest (e.g. after adding call-site extraction)
./assiter ingest --force /path/to/repo

# Ask the AI about the codebase
./assiter query "Which functions call the authentication handler?"
./assiter query "What would break if I change the User struct?" --symbol User

# Explore the graph
./assiter graph stats
./assiter graph search UserService
./assiter graph file /path/to/file.py
./assiter graph callers get_centre_booking

# Start REST API server
./assiter serve
```

### REST API

```bash
go run ./cmd/server

curl -X POST http://localhost:8080/ingest \
  -H 'Content-Type: application/json' \
  -d '{"dir": "/path/to/repo"}'

curl "http://localhost:8080/graph/search?name=UserService"
curl "http://localhost:8080/graph/node/<node-id>"
curl http://localhost:8080/graph/stats

curl -X POST http://localhost:8080/agent/query \
  -H 'Content-Type: application/json' \
  -d '{"question": "What does the auth package do?", "symbol": "Auth"}'
```

---

## MCP Server — for GitHub Copilot, Claude, Cursor

The MCP server exposes the knowledge graph as **tools** over stdio so any MCP-compatible agent can query it natively.

### Build

```bash
go build -o assiter-mcp ./cmd/mcp
```

### Configure VS Code (GitHub Copilot)

`.vscode/mcp.json` (already created in this repo):
```json
{
  "servers": {
    "assiter": {
      "type": "stdio",
      "command": "/absolute/path/to/assiter-mcp",
      "args": ["--config", "/absolute/path/to/assiter.yaml"]
    }
  }
}
```

To auto-ingest a project when the MCP server starts, set `preload.dir` in `assiter.yaml`:
```yaml
preload:
  dir: /home/user/myproject/src
  force: false   # true = always re-ingest even if unchanged
```

### Available MCP Tools

| Tool | Description |
|------|-------------|
| `ingest` | Parse and ingest a directory. Args: `dir`, `force` |
| `search_nodes` | Find symbols by name. Args: `name`, `type` (optional: Function/Method/Struct/…) |
| `search_callers` | Find all callers of a symbol. Args: `symbol` |
| `get_file_context` | List all symbols in a source file. Args: `file_path` |
| `get_node_context` | Node + all graph neighbours. Args: `node_id` |
| `graph_stats` | Node counts by type (verify ingestion coverage) |

### Example Copilot prompts once MCP is connected

```
Use get_file_context to show me all symbols in src/booking/services.py

Use search_callers to find everything that calls get_centre_booking

Use search_nodes to find the User struct and then get_node_context for its full graph context

Use ingest to index /home/user/myproject/src, then search_nodes for AuthService
```

---

## Project Structure

```
assiter/
├── cmd/
│   ├── cli/main.go          # CLI entrypoint (cobra)
│   ├── mcp/main.go          # MCP stdio server
│   └── server/main.go       # REST server entrypoint
├── internal/
│   ├── parser/              # Multi-language AST parsers
│   │   ├── parser.go        # Language dispatch + file walker
│   │   ├── golang.go        # Go (go/ast stdlib)
│   │   ├── python.go        # Python (Tree-sitter + call extraction)
│   │   ├── typescript.go    # TypeScript/TSX
│   │   ├── java.go          # Java
│   │   ├── rust.go          # Rust
│   │   ├── cpp.go           # C/C++
│   │   └── tsutil.go        # Shared Tree-sitter helpers + collectCalls()
│   ├── normalizer/          # Raw AST → UModel graph (+ call edges)
│   ├── umodel/              # Unified model types (Node, Edge, Graph)
│   ├── graph/               # Neo4j client, schema, queries
│   ├── ingestion/           # End-to-end pipeline (checksum dedup)
│   ├── agent/               # LLM agent (OpenAI / Copilot / custom)
│   └── api/                 # Gin REST handlers
├── .vscode/mcp.json         # VS Code MCP server config
├── assiter.example.yaml     # Safe config template
├── assiter.yaml             # Local config (gitignored)
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
| Symbol    | Named symbol referenced via CALLS edges (cross-file) |

## Supported Languages

| Language   | Parser strategy |
|------------|----------------|
| Go         | `go/ast` standard library (full AST) |
| Python     | Tree-sitter + call extraction |
| TypeScript | Tree-sitter |
| Java       | Tree-sitter |
| Rust       | Tree-sitter |
| C/C++      | Tree-sitter |


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

Copy the example and edit your local `assiter.yaml`:
```bash
cp assiter.example.yaml assiter.yaml
```

`assiter.yaml` is local-only and ignored by Git.

Example (`assiter.example.yaml`):
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
├── assiter.example.yaml     # Safe config template for sharing
├── assiter.yaml             # Local config (gitignored)
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
