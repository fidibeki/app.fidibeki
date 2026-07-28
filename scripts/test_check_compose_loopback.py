#!/usr/bin/env python3

from __future__ import annotations

import unittest

from check_compose_loopback import STATEFUL_SERVICES, validate_stateful_ports


def compose_config(
    ports_by_service: dict[str, list[dict[str, object]]] | None = None,
) -> dict[str, object]:
    ports_by_service = ports_by_service or {}
    return {
        "services": {
            service: {"ports": ports_by_service.get(service, [])}
            for service in STATEFUL_SERVICES
        }
    }


class ValidateStatefulPortsTest(unittest.TestCase):
    def test_accepts_unpublished_services(self) -> None:
        self.assertEqual(validate_stateful_ports(compose_config()), [])

    def test_accepts_ipv4_and_ipv6_loopback(self) -> None:
        config = compose_config(
            {
                "postgres": [
                    {"host_ip": "127.0.0.1", "published": "5433", "target": 5432}
                ],
                "minio": [
                    {"host_ip": "::1", "published": "9000", "target": 9000},
                    {"host_ip": "127.0.0.1", "published": "9001", "target": 9001},
                ],
            }
        )

        self.assertEqual(validate_stateful_ports(config), [])

    def test_rejects_omitted_host_address(self) -> None:
        config = compose_config(
            {"redis": [{"published": "6380", "target": 6379}]}
        )

        self.assertEqual(
            validate_stateful_ports(config),
            ["redis: <all interfaces>:6380->6379 is not loopback-bound"],
        )

    def test_rejects_ipv4_and_ipv6_wildcards(self) -> None:
        config = compose_config(
            {
                "postgres": [
                    {"host_ip": "0.0.0.0", "published": "5433", "target": 5432}
                ],
                "pgbouncer": [
                    {"host_ip": "::", "published": "6432", "target": 6432}
                ],
            }
        )

        self.assertEqual(
            validate_stateful_ports(config),
            [
                "postgres: 0.0.0.0:5433->5432 is not loopback-bound",
                "pgbouncer: :::6432->6432 is not loopback-bound",
            ],
        )

    def test_rejects_one_unsafe_mapping_among_safe_mappings(self) -> None:
        config = compose_config(
            {
                "minio": [
                    {"host_ip": "127.0.0.1", "published": "9000", "target": 9000},
                    {"host_ip": "192.168.1.10", "published": "9001", "target": 9001},
                ]
            }
        )

        self.assertEqual(
            validate_stateful_ports(config),
            ["minio: 192.168.1.10:9001->9001 is not loopback-bound"],
        )

    def test_rejects_missing_expected_service(self) -> None:
        config = compose_config()
        del config["services"]["redis"]  # type: ignore[index]

        self.assertEqual(
            validate_stateful_ports(config),
            ["redis: expected stateful service is missing from rendered configuration"],
        )

    def test_rejects_malformed_services_and_ports(self) -> None:
        self.assertEqual(
            validate_stateful_ports({}),
            ["rendered configuration has no services object"],
        )

        config = compose_config()
        config["services"]["postgres"]["ports"] = "5433:5432"  # type: ignore[index]
        self.assertEqual(
            validate_stateful_ports(config),
            ["postgres: rendered ports value is not a list"],
        )


if __name__ == "__main__":
    unittest.main()
