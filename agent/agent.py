import json
import os
import time
import uuid
from collections.abc import AsyncIterator, Awaitable, Callable
from contextlib import asynccontextmanager
from pathlib import Path

import anyio
import httpx2
from anthropic import AsyncAnthropic
from anthropic.types import Message, MessageParam, ToolParam, ToolResultBlockParam
from dotenv import load_dotenv
from langfuse import get_client, observe, propagate_attributes
from mcp import Client, StdioServerParameters, Tool
from mcp.client.streamable_http import streamable_http_client
from mcp.types import TextContent
from opentelemetry.instrumentation.anthropic import AnthropicInstrumentor

load_dotenv("../.env")
langfuse = get_client()
AnthropicInstrumentor().instrument()
BIN = Path(__file__).parent.parent / "bin" / "easypost-mcp"
SERVER = StdioServerParameters(
    command=str(BIN),
    env={"EASYPOST_API_KEY": os.environ["EASYPOST_API_KEY"]},
)
MODEL = os.environ.get("MODEL", "claude-haiku-5-5")
MAX_TURNS = 10
SYSTEM = """
You are an EasyPost operator. You can check delivery rates, track shipments, and buy labels.

If the user provides a deadline, call 'get_rates' with the 'max_days' parameter. Always offer the cheapest option that fits the user's criteria.

When displaying options, clearly show the following details:
- Carrier
- Service
- Price
- Days
- rate_id

Do not call 'buy_label' until the user has explicitly confirmed their chosen tariff.
"""


def log_turn(
    turn: int,
    r: Message,
    llm_ms: int,
    tool_parts: list[str],
    run_id: str,
    tool_log: list[dict],
) -> None:
    tools = ", ".join(tool_parts) if tool_parts else "-"
    print(
        f"turn {turn}: {r.stop_reason} | llm {llm_ms} ms | in={r.usage.input_tokens} out={r.usage.output_tokens} | tools: {tools}"
    )
    info = {
        "run_id": run_id,
        "turn": turn,
        "llm_ms": llm_ms,
        "input_tokens": r.usage.input_tokens,
        "output_tokens": r.usage.output_tokens,
        "tools": tool_log,
    }
    with open("runs.jsonl", "a") as f:
        f.write(json.dumps(info) + "\n")


@asynccontextmanager
async def remote_client(url: str, token: str) -> AsyncIterator[Client]:
    async with httpx2.AsyncClient(
        headers={"Authorization": f"Bearer {token}"},
        timeout=httpx2.Timeout(30.0, read=300.0),
    ) as http:
        transport = streamable_http_client(url, http_client=http)
        async with Client(transport, mode="2026-07-28") as mcp:
            yield mcp


@observe(name="agent-run", capture_input=False)
async def run_agent(
    mcp: Client,
    claude_tools: list[ToolParam],
    messages: list[MessageParam],
    tools_by_name: dict[str, Tool],
    approve: Callable[[str], Awaitable[bool]],
    last_rates: list[dict],
) -> str:
    llm = AsyncAnthropic()
    run_id = uuid.uuid4().hex
    langfuse.update_current_span(input=messages[0]["content"])

    with propagate_attributes(
        session_id=run_id, tags=["easypost-agent"], metadata={"model": MODEL}
    ):
        for turn in range(MAX_TURNS):
            print("Claude is thinking...", flush=True)
            t0 = time.perf_counter()
            r = await llm.messages.create(
                model=MODEL,
                max_tokens=1024,
                messages=messages,
                tools=claude_tools,
                system=SYSTEM,
            )
            llm_ms = int((time.perf_counter() - t0) * 1000)

            messages.append({"role": "assistant", "content": r.content})
            tool_log: list[dict] = []

            if r.stop_reason != "tool_use":
                log_turn(turn, r, llm_ms, [], run_id, tool_log)
                answer = "\n".join(b.text for b in r.content if b.type == "text")
                langfuse.update_current_span(output=answer)
                return answer
            results: list[ToolResultBlockParam] = []
            tool_parts: list[str] = []
            for c in r.content:
                if c.type != "tool_use":
                    continue
                t1 = time.perf_counter()
                if needs_approval(tools_by_name[c.name]):
                    summary = describe_rate(last_rates, str(c.input.get("rate_id")))
                    if summary is None or not await approve(summary):
                        if summary is None:
                            print("Unknown rate_id")
                            results.append(
                                {
                                    "type": "tool_result",
                                    "tool_use_id": c.id,
                                    "content": "Unknown rate_id",
                                    "is_error": True,
                                }
                            )
                            continue
                        else:
                            results.append(
                                {
                                    "type": "tool_result",
                                    "tool_use_id": c.id,
                                    "content": "User declined",
                                    "is_error": True,
                                }
                            )
                            print(f"-> {c.name} DECLINED")
                            continue
                with langfuse.start_as_current_observation(
                    name=c.name, as_type="tool", input=c.input
                ) as span:
                    try:
                        print(f"calling {c.name}...", flush=True)
                        res = await mcp.call_tool(c.name, c.input)
                        if c.name == "get_rates" and res.structured_content is not None:
                            last_rates[:] = res.structured_content["rates"]
                        is_error = bool(res.is_error)
                        text = "\n".join(
                            part.text
                            for part in res.content
                            if isinstance(part, TextContent)
                        )
                    except Exception as e:
                        text, is_error = str(e), True
                    span.update(output=text, level="ERROR" if is_error else "DEFAULT")
                tool_ms = int((time.perf_counter() - t1) * 1000)

                tool_parts.append(
                    f"{c.name} {tool_ms} ms" + (" ERROR" if is_error else "")
                )
                tool_log.append({"name": c.name, "ms": tool_ms, "is_error": is_error})
                results.append(
                    {
                        "type": "tool_result",
                        "tool_use_id": c.id,
                        "content": text,
                        "is_error": is_error,
                    }
                )

            log_turn(turn, r, llm_ms, tool_parts, run_id, tool_log)
            messages.append({"role": "user", "content": results})

    return "(MAX_TURNS reached)"


def describe_rate(rates: list[dict], rate_id: str) -> str | None:
    for r in rates:
        if r["id"] == rate_id:
            return f"{r['carrier']} {r['service']} ${r['rate']} ({r['delivery_days']} days) [{rate_id}]"
    return None


async def ask(summary: str) -> bool:
    answer = await anyio.to_thread.run_sync(input, f"{summary}\nApprove? [y/N] ")
    return answer.strip().lower() == "y"


def needs_approval(tool: Tool) -> bool:
    return not (tool.annotations and tool.annotations.read_only_hint)


async def run(mcp: Client) -> None:
    list_tools: list[Tool] = (await mcp.list_tools()).tools
    claude_tools: list[ToolParam] = [
        {
            "name": t.name,
            "description": t.description or "",
            "input_schema": t.input_schema,
        }
        for t in list_tools
    ]
    messages: list[MessageParam] = [
        {
            "role": "user",
            "content": "Rates for a 2 lb box 10x8x4 in. Sender name: EasyPost, phone 4153334445, 417 Montgomery St, San Francisco CA 94104. "
            "Recipient name: Dr. Steve Brule, phone 8573875756, 179 N Harbor Dr, Redondo Beach CA 90277. "
            "FedEx only,cheapest rate that arrives within 3 days (3 days is fine), buy the cheapest",
        }
    ]
    tools_by_name: dict[str, Tool] = {t.name: t for t in list_tools}
    last_rates: list[dict] = []
    summary = await run_agent(
        mcp, claude_tools, messages, tools_by_name, ask, last_rates
    )
    print(summary)
    reply = await anyio.to_thread.run_sync(input, "> ")
    messages.append({"role": "user", "content": reply})
    print(await run_agent(mcp, claude_tools, messages, tools_by_name, ask, last_rates))


async def main() -> None:
    if os.environ.get("MCP_URL"):
        url = os.environ["MCP_URL"] + "/mcp"
        async with remote_client(url, os.environ["MCP_TOKEN"]) as mcp:
            await run(mcp)
    else:
        async with Client(SERVER, mode="2026-07-28") as mcp:
            await run(mcp)


if __name__ == "__main__":
    anyio.run(main)
    langfuse.flush()
