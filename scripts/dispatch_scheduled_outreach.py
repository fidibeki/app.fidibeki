#!/usr/bin/env python3
"""Dispatch up to two pre-approved, individual cold emails on a weekday.

This is intentionally a small local scheduler, not bulk-email infrastructure.
The private queue and send register are ignored by Git.
"""

from __future__ import annotations

import argparse
import csv
import fcntl
import json
import smtplib
import ssl
import subprocess
import sys
from datetime import datetime, timezone
from email.message import EmailMessage
from pathlib import Path

from validate_outreach_headers import from_header


ROOT = Path(__file__).resolve().parent.parent
PRIVATE = ROOT / ".outreach-private"
QUEUE_PATH = PRIVATE / "scheduled-queue.json"
REGISTER_PATH = PRIVATE / "send-register.csv"
SUPPRESSION_PATH = PRIVATE / "suppression.csv"
LOCK_PATH = PRIVATE / "scheduled-dispatch.lock"
ACCOUNT = "ines.algo@fidibeki.com"
BCC = "acidiney.dias@fidibeki.com"
DAILY_LIMIT = 2


def load_json(path: Path) -> list[dict[str, str]]:
    with path.open(encoding="utf-8") as handle:
        data = json.load(handle)
    if not isinstance(data, list):
        raise ValueError("scheduled queue must be a JSON list")
    return data


def load_csv(path: Path) -> list[dict[str, str]]:
    with path.open(newline="", encoding="utf-8") as handle:
        return list(csv.DictReader(handle))


def valid_from_header() -> None:
    message = EmailMessage()
    message["From"] = from_header()
    if len(message["From"].addresses) != 1:
        raise ValueError("From header must contain exactly one address")
    if message["From"].addresses[0].addr_spec != ACCOUNT:
        raise ValueError("From header sender mismatch")


def password() -> str:
    return subprocess.check_output(
        ["security", "find-generic-password", "-a", ACCOUNT, "-s", "fidibeki-cmo-mail", "-w"],
        text=True,
    ).rstrip("\r\n")


def record_send(item: dict[str, str], timestamp: str) -> None:
    with REGISTER_PATH.open("a", newline="", encoding="utf-8") as handle:
        writer = csv.writer(handle)
        writer.writerow(
            [
                timestamp,
                item["recipient"],
                item["company"],
                item["source_url"],
                item["subject"],
                ACCOUNT,
                BCC,
                "accepted_by_smtp_scheduled_initial_touch",
                "Scheduled weekday first touch; Address-based From header; BCC verified in SMTP envelope; no tracking; no retry or duplicate-send",
            ]
        )


def message_for(item: dict[str, str]) -> EmailMessage:
    message = EmailMessage()
    message["From"] = from_header()
    message["To"] = item["recipient"]
    message["Subject"] = item["subject"]
    message["Reply-To"] = ACCOUNT
    message.set_content(item["body"].rstrip() + "\n\n" + item["footer"].strip())
    return message


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    now = datetime.now(timezone.utc)
    if now.astimezone().weekday() >= 5:
        print("scheduled_outreach=skipped_weekend")
        return 0
    valid_from_header()
    queue = load_json(QUEUE_PATH)
    register = load_csv(REGISTER_PATH)
    suppressed = {row["email"].strip().lower() for row in load_csv(SUPPRESSION_PATH) if row.get("email")}
    contacted = {row["recipient"].strip().lower() for row in register if row.get("recipient")}
    today = now.date().isoformat()
    sent_today = sum(1 for row in register if row.get("sent_at_utc", "").startswith(today) and row.get("send_status", "").startswith("accepted_by_smtp_scheduled"))
    slots = max(0, DAILY_LIMIT - sent_today)
    if slots == 0:
        print("scheduled_outreach=daily_limit_reached")
        return 0
    candidates: list[dict[str, str]] = []
    for item in queue:
        required = {"recipient", "company", "source_url", "subject", "body", "footer", "state"}
        if not required.issubset(item):
            raise ValueError("scheduled queue item is missing required fields")
        recipient = item["recipient"].strip().lower()
        if item["state"] != "ready":
            continue
        if recipient in suppressed:
            print(f"scheduled_outreach=skipped_suppressed recipient={item['recipient']}")
            continue
        if recipient in contacted:
            print(f"scheduled_outreach=skipped_already_contacted recipient={item['recipient']}")
            continue
        candidates.append(item)
        if len(candidates) >= slots:
            break

    if args.dry_run:
        print(f"scheduled_outreach=dry_run candidates={len(candidates)} slots={slots}")
        for item in candidates:
            print(f"would_send={item['recipient']} subject={item['subject']}")
        return 0
    if not candidates:
        print("scheduled_outreach=no_eligible_contacts")
        return 0

    with LOCK_PATH.open("w", encoding="utf-8") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        with smtplib.SMTP("mx.lumi.ao", 587, timeout=30) as smtp:
            smtp.ehlo()
            smtp.starttls(context=ssl.create_default_context())
            smtp.ehlo()
            smtp.login(ACCOUNT, password())
            for item in candidates:
                refused = smtp.send_message(message_for(item), from_addr=ACCOUNT, to_addrs=[item["recipient"], BCC])
                if refused:
                    raise RuntimeError(f"SMTP refused {item['recipient']}")
                timestamp = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
                record_send(item, timestamp)
                print(f"scheduled_outreach=accepted recipient={item['recipient']}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"scheduled_outreach=failed error={error}", file=sys.stderr)
        raise
