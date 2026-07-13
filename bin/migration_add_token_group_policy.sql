-- Token-level ordered group policy for API keys.
-- Compatible with SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6.
-- Run this file once before deploying the application version that uses it.

ALTER TABLE tokens ADD COLUMN group_policy TEXT;
