import json
import sys
from pathlib import Path

import anyio
from agent import BIN, run_agent
from anthropic.types import MessageParam, ToolParam
from evals.fake_easypost import start_fake
from mcp import Client, StdioServerParameters, Tool

MIN_PASS_RATE = 0.8


async def always_yes(summary: str) -> bool:
    return True


def summarize(results: list[list[bool]]) -> tuple[float, int]:
    total = sum(len(r) for r in results)
    passed = sum(sum(r) for r in results)
    all_ok = sum(all(r) for r in results)
    if total == 0:
        return 0.0, 0
    return passed / total, all_ok


async def run_case(case: dict) -> str | None:
    url, bought = start_fake(case)
    params = StdioServerParameters(
        env={"EASYPOST_API_KEY": "EZTKfake", "EASYPOST_BASE_URL": url},
        command=str(BIN),
        mode="2026-07-28",
    )
    async with Client(params) as mcp:
        list_tools: list[Tool] = (await mcp.list_tools()).tools
        claude_tools: list[ToolParam] = [
            {
                "name": t.name,
                "description": t.description or "",
                "input_schema": t.input_schema,
            }
            for t in list_tools
        ]
        tools_by_name: dict[str, Tool] = {t.name: t for t in list_tools}
        last_rates: list[dict] = []
        messages: list[MessageParam] = [{"role": "user", "content": case["prompt"]}]

        await run_agent(
            mcp,
            claude_tools,
            messages,
            tools_by_name,
            approve=always_yes,
            last_rates=last_rates,
        )
        messages.append({"role": "user", "content": "Yes, buy the one you proposed."})
        await run_agent(
            mcp,
            claude_tools,
            messages,
            tools_by_name,
            approve=always_yes,
            last_rates=last_rates,
        )

        return bought.get("rate_id")


def exit_code(pass_rate: float, minimum: float = MIN_PASS_RATE) -> int:
    if pass_rate >= minimum:
        return 0
    return 1


async def main() -> None:
    cases = json.loads(Path("evals/cases.json").read_text())
    results: list[list[bool]] = []

    for case in cases:
        runs: list[bool] = []
        for _ in range(3):
            bought = await run_case(case)
            runs.append(bought == case["rate"]["id"])
        results.append(runs)
        print(case["id"])
        rate, clean = summarize(results)
        print(f" pass {rate:.0%} · pass^3 {clean}/{len(cases)}")
        sys.exit(exit_code(rate))


if __name__ == "__main__":
    anyio.run(main)
