#!/usr/bin/env python3
"""Fail closed if the cold-email From header is not RFC 5322-safe."""

from email.headerregistry import Address
from email.message import EmailMessage


DISPLAY_NAME = "Inês, co-founder at Fidibeki"
ADDRESS = "ines.algo@fidibeki.com"


def from_header() -> Address:
    local, domain = ADDRESS.split("@", 1)
    return Address(display_name=DISPLAY_NAME, username=local, domain=domain)


def main() -> None:
    message = EmailMessage()
    message["From"] = from_header()
    rendered = str(message["From"])
    expected = '"Inês, co-founder at Fidibeki" <ines.algo@fidibeki.com>'
    if rendered != expected:
        raise SystemExit(f"unsafe From header: {rendered!r}")
    print("outreach_from_header=valid")


if __name__ == "__main__":
    main()
