ALTER TABLE tenant_members
    ADD COLUMN locale text,
    ADD COLUMN timezone text,
    ADD CONSTRAINT tenant_members_locale_valid CHECK (
        locale IS NULL OR locale IN ('en', 'pt', 'pt-AO', 'pt-PT', 'en-XA')
    ),
    ADD CONSTRAINT tenant_members_timezone_valid CHECK (
        timezone IS NULL OR (
            char_length(timezone) BETWEEN 1 AND 100
            AND timezone ~ '^[A-Za-z0-9._+-]+(/[A-Za-z0-9._+-]+)*$'
        )
    );
