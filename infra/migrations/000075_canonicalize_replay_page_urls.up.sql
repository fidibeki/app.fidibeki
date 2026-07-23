-- Existing replay session page URLs may still carry credentials, a query string, or a
-- fragment captured before the widget/backend enforced URL canonicalization (issue #53).
-- This strips only those well-defined URL syntax components (not a general identity-text
-- redaction heuristic) so persisted replay metadata matches the origin+pathname contract.
UPDATE sessions
SET page_url = regexp_replace(
    regexp_replace(
        regexp_replace(page_url, '#.*$', ''),
        '\?.*$', ''
    ),
    '^(https?://)[^/@]*@', '\1'
)
WHERE page_url IS NOT NULL
  AND (page_url ~ '[?#]' OR page_url ~ '^https?://[^/@]*@');
