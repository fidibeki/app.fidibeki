#!/usr/bin/env python3
"""Reject stateful development services published beyond loopback."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parent.parent
DEFAULT_COMPOSE_FILE = ROOT / "infra" / "docker-compose.yml"
STATEFUL_SERVICES = ("postgres", "pgbouncer", "redis", "minio")
LOOPBACK_HOST_IPS = frozenset({"127.0.0.1", "::1"})


def validate_stateful_ports(config: dict[str, Any]) -> list[str]:
    services = config.get("services")
    if not isinstance(services, dict):
        return ["rendered configuration has no services object"]

    violations: list[str] = []
    for service_name in STATEFUL_SERVICES:
        service = services.get(service_name)
        if not isinstance(service, dict):
            violations.append(
                f"{service_name}: expected stateful service is missing from rendered configuration"
            )
            continue

        ports = service.get("ports", [])
        if not isinstance(ports, list):
            violations.append(f"{service_name}: rendered ports value is not a list")
            continue

        for port in ports:
            if not isinstance(port, dict):
                violations.append(f"{service_name}: rendered port entry is not an object")
                continue

            host_ip = port.get("host_ip")
            if host_ip in LOOPBACK_HOST_IPS:
                continue

            published = port.get("published", "?")
            target = port.get("target", "?")
            displayed_host = host_ip if host_ip else "<all interfaces>"
            violations.append(
                f"{service_name}: {displayed_host}:{published}->{target} is not loopback-bound"
            )

    return violations


def render_compose(compose_file: Path) -> dict[str, Any]:
    result = subprocess.run(
        [
            "docker",
            "compose",
            "--file",
            str(compose_file),
            "config",
            "--format",
            "json",
        ],
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        message = result.stderr.strip() or "docker compose config failed"
        raise RuntimeError(message)

    try:
        rendered = json.loads(result.stdout)
    except json.JSONDecodeError as error:
        raise RuntimeError(f"docker compose returned invalid JSON: {error}") from error
    if not isinstance(rendered, dict):
        raise RuntimeError("docker compose returned a non-object configuration")
    return rendered


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--compose-file",
        type=Path,
        default=DEFAULT_COMPOSE_FILE,
        help="Compose file to render and validate",
    )
    args = parser.parse_args(argv)

    try:
        violations = validate_stateful_ports(render_compose(args.compose_file))
    except (OSError, RuntimeError) as error:
        print(f"compose_loopback_check=error detail={error}", file=sys.stderr)
        return 2

    if violations:
        print("Stateful development ports must be unpublished or loopback-bound:", file=sys.stderr)
        for violation in violations:
            print(f"- {violation}", file=sys.stderr)
        return 1

    print("compose_loopback_check=passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
