#!/usr/bin/env python3
"""Run the one-time Z.AI reasoning-effort acceptance matrix.

This opt-in probe records only sanitized protocol outcomes. It is not imported
by production code and must never run from routine test or check entrypoints.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import hashlib
import json
import os
import pathlib
import signal
import ssl
import sys
import time
import urllib.error
import urllib.request
from dataclasses import asdict, dataclass
from typing import Any


ACCESS_PRODUCTS = {
    "general_api": "https://api.z.ai/api/paas/v4",
    "coding_plan": "https://api.z.ai/api/coding/paas/v4",
}
CORE_MODELS = (
    "glm-4.5",
    "glm-4.5-air",
    "glm-4.6",
    "glm-4.7",
    "glm-5",
    "glm-5-turbo",
    "glm-5.1",
    "glm-5.2",
)
EFFORTS = ("minimal", "low", "medium", "high", "xhigh", "max")
PARAMETER_ERROR_CODES = {1210, 1213, 1214, 1215}


@dataclass(frozen=True)
class ProbeCase:
    access: str
    model: str
    thinking: str
    reasoning_effort: str | None
    stream: bool
    tools: bool
    variant: str
    pass_number: int


@dataclass
class ProbeResult:
    access: str
    endpoint_host: str
    model: str
    thinking: str
    reasoning_effort: str | None
    stream: bool
    tools: bool
    variant: str
    pass_number: int
    http_status: int | None
    provider_code: int | None
    terminal: str | None
    accepted: bool
    rejected: bool
    inconclusive_reason: str | None
    attempts: int


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=pathlib.Path)
    parser.add_argument("--workers", type=int, default=4)
    parser.add_argument("--retries", type=int, default=2)
    parser.add_argument("--passes", type=int, default=2)
    parser.add_argument("--pass-gap-seconds", type=int, default=2)
    parser.add_argument("--timeout-seconds", type=int, default=120)
    parser.add_argument("--journal", type=pathlib.Path)
    parser.add_argument("--accesses", default=",".join(ACCESS_PRODUCTS))
    parser.add_argument("--models", default=",".join(CORE_MODELS))
    parser.add_argument("--efforts", default=",".join(EFFORTS))
    parser.add_argument("--variants", default="baseline,effort,disabled,disabled_conflict,unknown_sentinel,tool,tool_stream")
    parser.add_argument("--dry-run", action="store_true")
    return parser.parse_args()


def authorization() -> str:
    api_key = os.environ.get("ZAI_API_KEY", "").strip()
    if not api_key:
        raise SystemExit("ZAI_API_KEY is required")
    return f"Bearer {api_key}"


def request_json(url: str, bearer: str, timeout: int) -> tuple[int, bytes]:
    request = urllib.request.Request(url, headers={"Authorization": bearer})
    try:
        with urllib.request.urlopen(request, timeout=timeout, context=ssl.create_default_context()) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def inventory(access: str, base_url: str, bearer: str, timeout: int) -> dict[str, Any]:
    status, raw = request_json(f"{base_url}/models", bearer, timeout)
    body = decode_json(raw)
    models = sorted(
        item["id"]
        for item in body.get("data", [])
        if isinstance(item, dict) and isinstance(item.get("id"), str)
    )
    return {
        "access": access,
        "http_status": status,
        "models": models,
        "provider_code": provider_code(body),
    }


def csv_values(raw: str) -> tuple[str, ...]:
    return tuple(value.strip() for value in raw.split(",") if value.strip())


def matrix_cases(
    inventories: list[dict[str, Any]],
    passes: int,
    models: tuple[str, ...],
    efforts: tuple[str, ...],
    variants: set[str],
) -> list[ProbeCase]:
    cases: list[ProbeCase] = []
    for pass_number in range(1, passes + 1):
        for inventory_row in inventories:
            access = inventory_row["access"]
            available = set(inventory_row["models"])
            for model in models:
                if model not in available:
                    continue
                if "baseline" in variants:
                    cases.append(ProbeCase(access, model, "enabled", None, False, False, "baseline", pass_number))
                if "effort" in variants:
                    for effort in efforts:
                        cases.append(ProbeCase(access, model, "enabled", effort, False, False, "effort", pass_number))
                if "disabled" in variants:
                    cases.append(ProbeCase(access, model, "disabled", None, False, False, "disabled", pass_number))
                if "disabled_conflict" in variants:
                    cases.append(ProbeCase(access, model, "disabled", "high", False, False, "disabled_conflict", pass_number))
                if "unknown_sentinel" in variants:
                    cases.append(ProbeCase(access, model, "enabled", None, False, False, "unknown_sentinel", pass_number))
                if "tool" in variants:
                    cases.append(ProbeCase(access, model, "enabled", "high", False, True, "tool", pass_number))
                if "tool_stream" in variants:
                    cases.append(ProbeCase(access, model, "enabled", "high", True, True, "tool_stream", pass_number))
    return cases


def payload(case: ProbeCase) -> dict[str, Any]:
    body: dict[str, Any] = {
        "model": case.model,
        "messages": [{"role": "user", "content": "Reply with exactly OK."}],
        "thinking": {"type": case.thinking},
        "stream": case.stream,
        "max_tokens": 64,
        "temperature": 0,
    }
    if case.reasoning_effort is not None:
        body["reasoning_effort"] = case.reasoning_effort
    if case.variant == "unknown_sentinel":
        body["swobu_contract_sentinel"] = True
    if case.tools:
        body["messages"] = [{"role": "user", "content": "Call report_ok with an empty object."}]
        body["tools"] = [
            {
                "type": "function",
                "function": {
                    "name": "report_ok",
                    "description": "Report that the contract probe completed.",
                    "parameters": {"type": "object", "properties": {}, "additionalProperties": False},
                },
            }
        ]
        body["tool_choice"] = "auto"
    return body


def run_case(case: ProbeCase, bearer: str, timeout: int, retries: int) -> ProbeResult:
    url = f"{ACCESS_PRODUCTS[case.access]}/chat/completions"
    attempts = 0
    last: ProbeResult | None = None
    while attempts <= retries:
        attempts += 1
        last = execute(case, url, bearer, timeout, attempts)
        if last.accepted or last.rejected:
            return last
        if attempts <= retries:
            time.sleep(attempts)
    assert last is not None
    return last


def execute(case: ProbeCase, url: str, bearer: str, timeout: int, attempts: int) -> ProbeResult:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload(case), separators=(",", ":")).encode(),
        headers={"Authorization": bearer, "Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=timeout, context=ssl.create_default_context()) as response:
            raw = response.read()
            return classify(case, response.status, raw, attempts)
    except urllib.error.HTTPError as error:
        return classify(case, error.code, error.read(), attempts)
    except (TimeoutError, urllib.error.URLError, ConnectionError) as error:
        return base_result(case, attempts, inconclusive_reason=type(error).__name__)


def classify(case: ProbeCase, status: int, raw: bytes, attempts: int) -> ProbeResult:
    body = decode_stream_or_json(raw, case.stream)
    code = provider_code(body)
    terminal = terminal_state(body, case.stream)
    normal_terminal = terminal in {"stop", "tool_calls"}
    if 200 <= status < 300 and code is None and normal_terminal:
        return base_result(case, attempts, status, code, terminal, accepted=True)
    if status == 400 and (code in PARAMETER_ERROR_CODES or code is None):
        return base_result(case, attempts, status, code, terminal, rejected=True)
    reason = f"http_{status}"
    if code is not None:
        reason = f"provider_{code}"
    if 200 <= status < 300 and not normal_terminal:
        reason = "missing_normal_terminal"
    return base_result(case, attempts, status, code, terminal, inconclusive_reason=reason)


def decode_stream_or_json(raw: bytes, stream: bool) -> dict[str, Any]:
    if not stream:
        return decode_json(raw)
    events: list[dict[str, Any]] = []
    for line in raw.decode("utf-8", "replace").splitlines():
        if not line.startswith("data:"):
            continue
        data = line[5:].strip()
        if not data or data == "[DONE]":
            continue
        decoded = decode_json(data.encode())
        if decoded:
            events.append(decoded)
    return {"events": events}


def decode_json(raw: bytes) -> dict[str, Any]:
    try:
        value = json.loads(raw.decode("utf-8", "replace"))
    except json.JSONDecodeError:
        return {}
    return value if isinstance(value, dict) else {}


def provider_code(body: dict[str, Any]) -> int | None:
    for candidate in (body.get("code"), nested_error(body).get("code")):
        try:
            return int(candidate)
        except (TypeError, ValueError):
            continue
    return None


def nested_error(body: dict[str, Any]) -> dict[str, Any]:
    error = body.get("error")
    return error if isinstance(error, dict) else {}


def terminal_state(body: dict[str, Any], stream: bool) -> str | None:
    if stream:
        events = body.get("events", [])
        for event in events:
            choices = event.get("choices", []) if isinstance(event, dict) else []
            for choice in choices:
                finish = choice.get("finish_reason") if isinstance(choice, dict) else None
                if finish in {"stop", "tool_calls"}:
                    return finish
        return None
    choices = body.get("choices", [])
    for choice in choices:
        if not isinstance(choice, dict):
            continue
        finish = choice.get("finish_reason")
        if finish in {"stop", "tool_calls"}:
            return finish
    return None


def base_result(
    case: ProbeCase,
    attempts: int,
    http_status: int | None = None,
    code: int | None = None,
    terminal: str | None = None,
    accepted: bool = False,
    rejected: bool = False,
    inconclusive_reason: str | None = None,
) -> ProbeResult:
    return ProbeResult(
        access=case.access,
        endpoint_host="api.z.ai",
        model=case.model,
        thinking=case.thinking,
        reasoning_effort=case.reasoning_effort,
        stream=case.stream,
        tools=case.tools,
        variant=case.variant,
        pass_number=case.pass_number,
        http_status=http_status,
        provider_code=code,
        terminal=terminal,
        accepted=accepted,
        rejected=rejected,
        inconclusive_reason=inconclusive_reason,
        attempts=attempts,
    )


def source_sha256() -> str:
    return hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest()


def case_key(case: ProbeCase | ProbeResult) -> tuple[Any, ...]:
    return (
        case.pass_number,
        case.access,
        case.model,
        case.thinking,
        case.reasoning_effort,
        case.stream,
        case.tools,
        case.variant,
    )


def append_journal(path: pathlib.Path, event: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as journal:
        journal.write(json.dumps(event, sort_keys=True, separators=(",", ":")) + "\n")
        journal.flush()
        os.fsync(journal.fileno())


def load_journal(path: pathlib.Path) -> tuple[list[ProbeResult], list[dict[str, Any]]]:
    rows: dict[tuple[Any, ...], ProbeResult] = {}
    events: list[dict[str, Any]] = []
    if not path.exists():
        return [], events
    for line_number, line in enumerate(path.read_text().splitlines(), start=1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
        except json.JSONDecodeError as error:
            raise SystemExit(f"journal line {line_number} is invalid: {error}") from error
        events.append(event)
        if event.get("event") == "row_completed":
            result = ProbeResult(**event["result"])
            rows[case_key(result)] = result
    return list(rows.values()), events


def write_artifact(
    output: pathlib.Path,
    journal: pathlib.Path,
    inventories: list[dict[str, Any]],
    rows: list[ProbeResult],
    state: str,
) -> None:
    rows.sort(key=lambda row: (row.pass_number, row.access, row.model, row.variant, row.reasoning_effort or "", row.stream))
    artifact = {
        "schema_version": 2,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "state": state,
        "official_reference": "https://docs.z.ai/api-reference/llm/chat-completion",
        "probe_source": "scripts/zai-live-contract/reasoning_effort_probe.py",
        "probe_source_sha256": source_sha256(),
        "journal": str(journal),
        "inventories": inventories,
        "decision": decide(rows),
        "rows": [asdict(row) for row in rows],
    }
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + ".tmp")
    with temporary.open("w", encoding="utf-8") as destination:
        json.dump(artifact, destination, indent=2, sort_keys=True)
        destination.write("\n")
        destination.flush()
        os.fsync(destination.fileno())
    os.replace(temporary, output)


def decide(rows: list[ProbeResult]) -> dict[str, Any]:
    binding = [row for row in rows if row.variant in {"effort", "tool", "tool_stream"}]
    controls = [row for row in rows if row.variant not in {"effort", "tool", "tool_stream"}]
    rejected = [row for row in binding if row.rejected]
    inconclusive = [row for row in binding if not row.accepted and not row.rejected]
    outcome = "A" if binding and not rejected and not inconclusive else "B_or_incomplete"
    return {
        "outcome": outcome,
        "binding_rows": len(binding),
        "accepted": sum(row.accepted for row in binding),
        "rejected": len(rejected),
        "inconclusive": len(inconclusive),
        "control_rows": len(controls),
        "controls_accepted": sum(row.accepted for row in controls),
        "controls_rejected": sum(row.rejected for row in controls),
        "controls_inconclusive": sum(not row.accepted and not row.rejected for row in controls),
    }


def main() -> None:
    args = parse_args()
    selected_accesses = csv_values(args.accesses)
    unknown_accesses = set(selected_accesses) - set(ACCESS_PRODUCTS)
    if unknown_accesses:
        raise SystemExit(f"unknown accesses: {sorted(unknown_accesses)}")
    selected_models = csv_values(args.models)
    selected_efforts = csv_values(args.efforts)
    unknown_efforts = set(selected_efforts) - set(EFFORTS)
    if unknown_efforts:
        raise SystemExit(f"unknown efforts: {sorted(unknown_efforts)}")
    selected_variants = set(csv_values(args.variants))
    if args.dry_run:
        synthetic_inventories = [
            {"access": access, "models": list(selected_models)}
            for access in selected_accesses
        ]
        cases = matrix_cases(
            synthetic_inventories,
            args.passes,
            selected_models,
            selected_efforts,
            selected_variants,
        )
        print(json.dumps({
            "accesses": selected_accesses,
            "models": selected_models,
            "efforts": selected_efforts,
            "variants": sorted(selected_variants),
            "passes": args.passes,
            "planned_generation_rows": len(cases),
            "network_requests": 0,
        }, sort_keys=True))
        return

    bearer = authorization()
    journal = args.journal or args.output.with_suffix(args.output.suffix + ".jsonl")
    recovered_rows, prior_events = load_journal(journal)
    append_journal(journal, {
        "event": "run_started",
        "at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "probe_source_sha256": source_sha256(),
        "resumed_rows": len(recovered_rows),
        "settings": {
            "workers": args.workers,
            "retries": args.retries,
            "passes": args.passes,
            "pass_gap_seconds": args.pass_gap_seconds,
            "timeout_seconds": args.timeout_seconds,
        },
    })
    inventories = [
        inventory(access, base_url, bearer, args.timeout_seconds)
        for access, base_url in ACCESS_PRODUCTS.items()
        if access in selected_accesses
    ]
    for inventory_row in inventories:
        append_journal(journal, {
            "event": "inventory_recorded",
            "at": dt.datetime.now(dt.timezone.utc).isoformat(),
            "inventory": inventory_row,
        })
    cases = matrix_cases(inventories, args.passes, selected_models, selected_efforts, selected_variants)
    completed = {case_key(row) for row in recovered_rows}
    pending = [case for case in cases if case_key(case) not in completed]
    rows = recovered_rows
    write_artifact(args.output, journal, inventories, rows, "running")

    interrupted = False

    def request_stop(signum: int, _frame: Any) -> None:
        nonlocal interrupted
        interrupted = True
        append_journal(journal, {
            "event": "interruption_requested",
            "at": dt.datetime.now(dt.timezone.utc).isoformat(),
            "signal": signum,
            "persisted_rows": len(rows),
        })

    signal.signal(signal.SIGINT, request_stop)
    signal.signal(signal.SIGTERM, request_stop)

    try:
        for pass_number in range(1, args.passes + 1):
            if interrupted:
                break
            pass_cases = [case for case in pending if case.pass_number == pass_number]
            executor = concurrent.futures.ThreadPoolExecutor(max_workers=args.workers)
            futures = [executor.submit(run_case, case, bearer, args.timeout_seconds, args.retries) for case in pass_cases]
            try:
                for future in concurrent.futures.as_completed(futures):
                    result = future.result()
                    rows.append(result)
                    append_journal(journal, {
                        "event": "row_completed",
                        "at": dt.datetime.now(dt.timezone.utc).isoformat(),
                        "result": asdict(result),
                    })
                    write_artifact(args.output, journal, inventories, rows, "running")
                    if interrupted:
                        break
            finally:
                executor.shutdown(wait=not interrupted, cancel_futures=interrupted)
            if pass_number < args.passes and not interrupted:
                time.sleep(args.pass_gap_seconds)
    except KeyboardInterrupt:
        interrupted = True

    state = "interrupted" if interrupted else "completed"
    append_journal(journal, {
        "event": f"run_{state}",
        "at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "persisted_rows": len(rows),
        "planned_rows": len(cases),
    })
    write_artifact(args.output, journal, inventories, rows, state)
    print(json.dumps({"state": state, **decide(rows)}, sort_keys=True))
    if interrupted:
        raise SystemExit(130)


if __name__ == "__main__":
    main()
