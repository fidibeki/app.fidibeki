ALTER TABLE website_chat_visitors ADD COLUMN locale text NOT NULL DEFAULT '' CHECK (length(locale) <= 35 AND (locale = '' OR locale ~ '^[a-z]{2,3}(-[A-Z][a-z]{3})?(-([A-Z]{2}|[0-9]{3}))?$'));
