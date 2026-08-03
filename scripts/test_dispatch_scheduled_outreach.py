#!/usr/bin/env python3
"""Regression tests for the guarded outreach dispatcher."""

from __future__ import annotations

import csv
import importlib.util
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from datetime import datetime, timezone
from pathlib import Path


SCRIPT = Path(__file__).with_name("dispatch_scheduled_outreach.py")
SPEC = importlib.util.spec_from_file_location("dispatch_scheduled_outreach", SCRIPT)
assert SPEC and SPEC.loader
DISPATCH = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = DISPATCH
SPEC.loader.exec_module(DISPATCH)


class WednesdayDatetime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 8, 5, 10, 15, tzinfo=timezone.utc)


class ScheduledDispatchTests(unittest.TestCase):
    def test_dry_run_skips_already_contacted_queue_items(self):
        with tempfile.TemporaryDirectory() as directory:
            private = Path(directory)
            queue = private / "queue.json"
            register = private / "register.csv"
            suppression = private / "suppression.csv"
            queue.write_text(
                json.dumps(
                    [
                        {"recipient": "already@example.com", "company": "Already", "source_url": "https://example.com", "subject": "Already", "body": "Hi", "footer": "Footer", "state": "ready"},
                        {"recipient": "next@example.com", "company": "Next", "source_url": "https://example.com", "subject": "Next", "body": "Hi", "footer": "Footer", "state": "ready"},
                    ]
                )
            )
            with register.open("w", newline="") as handle:
                writer = csv.DictWriter(handle, fieldnames=["recipient", "sent_at_utc", "send_status"])
                writer.writeheader()
                writer.writerow({"recipient": "already@example.com", "sent_at_utc": "2026-08-04T10:15:00Z", "send_status": "accepted_by_smtp_scheduled_initial_touch"})
            suppression.write_text("email\n")

            originals = (DISPATCH.QUEUE_PATH, DISPATCH.REGISTER_PATH, DISPATCH.SUPPRESSION_PATH, DISPATCH.datetime, sys.argv[:])
            try:
                DISPATCH.QUEUE_PATH = queue
                DISPATCH.REGISTER_PATH = register
                DISPATCH.SUPPRESSION_PATH = suppression
                DISPATCH.datetime = WednesdayDatetime
                sys.argv = [str(SCRIPT), "--dry-run"]
                output = io.StringIO()
                with redirect_stdout(output):
                    self.assertEqual(DISPATCH.main(), 0)
            finally:
                DISPATCH.QUEUE_PATH, DISPATCH.REGISTER_PATH, DISPATCH.SUPPRESSION_PATH, DISPATCH.datetime, sys.argv = originals

            self.assertIn("would_send=next@example.com", output.getvalue())
            self.assertNotIn("already@example.com subject=", output.getvalue())


if __name__ == "__main__":
    unittest.main()
