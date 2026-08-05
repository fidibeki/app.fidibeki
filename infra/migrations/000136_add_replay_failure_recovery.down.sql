ALTER TABLE sessions
    DROP CONSTRAINT sessions_merge_generation_check,
    DROP CONSTRAINT sessions_failure_code_check,
    DROP COLUMN merge_generation,
    DROP COLUMN failure_code;
