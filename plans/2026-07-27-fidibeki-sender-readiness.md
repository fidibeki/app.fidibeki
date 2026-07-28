# Fidibeki Cold-Email Sender Readiness

**Checked:** 2026-07-27
**Status:** Pilot active — DKIM DNS repaired; three US messages accepted by SMTP
**Owner:** Inês, CMO

## What was verified

| Control         | Evidence                                                                   | Status                                  |
| --------------- | -------------------------------------------------------------------------- | --------------------------------------- |
| SMTP account    | Authenticated internal delivery accepted by `mx.lumi.ao:587` with STARTTLS | Ready                                   |
| IMAP account    | Internal test message reached the configured inbox                         | Ready                                   |
| SPF             | `fidibeki.com` publishes `v=spf1 a:mx.lumi.ao ~all`                        | Published; external pass still required |
| DMARC           | `_dmarc.fidibeki.com` publishes `v=DMARC1; p=none; ...`                    | Published; monitoring only              |
| DKIM signing    | Internal message contains `DKIM-Signature` with `d=fidibeki.com; s=dkim`   | Server signs                            |
| DKIM public key | `dkim._domainkey.fidibeki.com` now publishes a `v=DKIM1` RSA public key    | Published                               |

An external verifier test was accepted by SMTP, but no automated result arrived during the check window. That absence is not proof of authentication success. A DKIM-Signature header alone is also not proof that recipients can validate it; they need the matching public key in DNS. The matching public key is now published, so the launch can proceed only as a deliberately small pilot while bounce and reply signals are observed.

## DNS repair completed — retain this verification procedure

1. Retain the published DKIM public key at `dkim._domainkey.fidibeki.com` and do not alter its selector/value without coordinating the mail host.
2. At the next available independent recipient/header check, verify all of the following:
   - `spf=pass` for `fidibeki.com`;
   - `dkim=pass` with `header.d=fidibeki.com`;
   - `dmarc=pass` aligned to `fidibeki.com`.

3. Record the three results and date in the private outreach register.

## Operational controls required before delivery

The outreach register must be private (not committed to this repository) and contain, at minimum:

- recipient corporate email and source URL;
- company, country, relevant legal-basis/market check, and personalised observation;
- pre-send suppression check and timestamp;
- sent timestamp, subject, sender, and founder BCC confirmation;
- reply outcome; and
- opt-out timestamp/reason.

An unsubscribe, "no", or direct objection means permanent suppression for that business contact. Do not delete the record simply because it is an objection—the suppression record is needed to prevent future contact.

## Pilot sending constraint

The initial three individual US messages were accepted by SMTP on 2026-07-27. Do not bulk-send and do not add open/click tracking. Check bounce and reply outcomes before extending the list. The broader 10–15/day target begins only after clean authentication, no delivery problems, and a stable suppression process.

## No expansion until pilot evidence

The three first-touch US messages have been sent. Any new prospect or follow-up requires a suppression check, an individual draft, and evidence from this pilot; do not treat SMTP acceptance as evidence of inbox placement or customer interest.
