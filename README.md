# easypost-mcp

MCP server in Go for the EasyPost shipping API. Built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

| Tool | What it does | Annotations |
|---|---|---|
| `get_rates` | Creates a shipment, returns carrier rates cheapest first (optional `max_days`) | read-only |
| `buy_label` | Buys a label at a chosen rate, returns tracking code + label URL | destructive — call only after human approval |

Safety: production keys (`EZAK…`) are refused unless `ALLOW_PRODUCTION=1`. Demo runs on a free test key (`EZTK…`).

## Run
```bash
go test ./...
EASYPOST_API_KEY=EZTK... go run .              # stdio
EASYPOST_API_KEY=EZTK... go run . -http :8080  # streamable HTTP
```

## Status (demo №1, ≤10 working days)
- [x] Go MCP server: `get_rates`, `buy_label`, test-key guard, unit tests
- [ ] Real call against EasyPost test key
- [ ] TS agent: compare rates → propose cheapest under constraints → ask human → buy
- [ ] Deploy on AWS (HTTP endpoint behind auth)
- [ ] Evals: does the agent pick the right rate? (fixtures of rate lists + expected choice)
- [ ] README table: $/request, p95 latency; 2-min video
