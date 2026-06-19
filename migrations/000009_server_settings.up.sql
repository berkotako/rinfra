-- Server-wide key/value settings (e.g. the selected IaC backend: pulumi|terraform).
CREATE TABLE server_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
