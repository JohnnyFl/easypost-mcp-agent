# easypost-mcp

A Go MCP server over the EasyPost shipping API, plus a Python agent that finds the best rate for a request and **buys a label only after a human says yes**. Deployed on AWS Lambda with Terraform; every agent run is traced in Langfuse.

```
 human ──► agent (Python, host + MCP client) ──► Claude API
              │  approval gate [y/N] (in code)
              ▼  Streamable HTTP, Bearer token
        AWS Lambda Function URL ──► Go MCP server ──► EasyPost (test mode)
              ▲                                           
              └─ slog JSON ─► CloudWatch     agent traces ─► Langfuse
```

## Tools

Built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk).

| Tool | What it does | Annotations |
|---|---|---|
| `get_rates` | Creates a shipment, returns carrier rates cheapest first (optional `max_days`) | read-only |
| `buy_label` | Buys a label at a chosen rate, returns tracking code and label URL | destructive: only after human approval |
| `track_shipment` | Returns tracking status and events for a tracking code | read-only, open-world |

## Safety

Three layers, each enforced in code, not in the prompt:
1. **Test-key guard.** Production keys (`EZAK…`) are refused unless `ALLOW_PRODUCTION=1`.
2. **Approval gate in the host.** The agent asks `[y/N]` before any destructive tool, with the price taken from the server's data (not from the model's words). Unknown `rate_id` is rejected; default is deny.
3. **Endpoint auth.** The HTTP endpoint needs a Bearer token; the server refuses to start without one.

## Compatibility

Speaks MCP `2026-07-28` (stateless, no sessions) and the older handshake-based versions.

## Evals and observability

10 cases (2 held out) × 3 runs against a fake EasyPost; CI runs them on `main`. Results are in [Cost](#cost-per-task-and-model-comparison). Each agent run is one Langfuse trace with a span per Claude call and per tool call (tokens, cost, latency).

<!-- TODO: screenshot of one Langfuse trace (hide keys/ids): docs/trace.png -->

## Metrics

Measured against the deployed AWS Lambda (`eu-central-1`, Function URL), from Ukraine, with `scripts/latency.sh`; p50/p95 by nearest-rank.

| operation | N | p50 | p95 | cold start |
|---|---|---|---|---|
| `tools/list` | 50 | 0.11 s | 0.20 s | ≈0.6 s |
| `get_rates` (test EasyPost) | 30 | 1.69 s | 2.48 s | |

`tools/list` is mostly network round trip; `get_rates` time is dominated by the EasyPost call.

### Cost per task and model comparison

`$/task = avg_in × price_in / 1M + avg_out × price_out / 1M` (+ Lambda, a fraction of a cent; EasyPost is $0 in test mode).
Measured on the 10 eval cases × 3 runs (`agent/evals`), average tokens per task from `runs.jsonl`.

| model | pass rate | pass^3 | avg in / out tokens | $/task |
|---|---|---|---|---|
| `claude-haiku-5-5` ($0.10 / $0.50 per 1M) | 90% | 7/10 | 4309 / 463 | ≈ $0.0007 |
| `claude-sonnet-5-5` ($2 / $10 per 1M) | 90% | 9/10 | 4415 / 375 | ≈ $0.0126 |

Same average pass rate, but Sonnet is more reliable (pass^3) at ~19× the cost.

## Run

```bash
go test ./...
EASYPOST_API_KEY=EZTK... go run .                              # stdio
EASYPOST_API_KEY=EZTK... MCP_TOKEN=... go run . -http :8080    # Streamable HTTP
cd agent && uv run agent.py                                    # agent (needs ANTHROPIC_API_KEY in ../.env)
cd infra && terraform apply                                    # AWS Lambda (TF_VAR_easypost_api_key, TF_VAR_mcp_token)
```

## Demo

[Watch the 2-minute demo](https://youtu.be/fWUUsc_JGDc?si=DJj3ETehLDRkXu-N)

## Alternatives considered

- **MCP connector in the Claude API:** runs tools on Anthropic's side, so there is no place for a human approval gate. Fine for read-only agents.
- **SDK Tool Runner:** hides the loop; I wanted explicit control of approval, token logging and errors.
- **OAuth on the endpoint:** the right choice for many clients; a single shared bearer token is enough for one demo client.
- **ECS Express instead of Lambda:** the server is stateless (spec 2026-07-28), so Lambda scales to zero and costs nothing idle.

## Limits and next steps

Spec-compliant logging is `slog` to stderr only; no OpenTelemetry trace propagation across agent → server yet. The endpoint uses one shared bearer token instead of OAuth. Test mode only. Buying a USPS rate currently fails with `ProviderEndShipper address.name or address.company required` (USPS needs an EndShipper that the server does not send yet); FedEx rates buy fine.
