--
-- Copyright 2018 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

-- This structure is cribbed from the code of
-- https://github.com/antonlindstrom/pgstore
-- We use that module but prefer to keep the table creation
-- in our own package.
CREATE TABLE IF NOT EXISTS http_sessions (
	id BIGSERIAL PRIMARY KEY,
	key BYTEA,
	data BYTEA,
	created_on TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
	modified_on TIMESTAMPTZ,
	expires_on TIMESTAMPTZ);

-- The go package doesn't index on the expires_on or key fields; however, it
-- does make queries against them.  We add our own indices in hopes of keeping
-- things fast.
CREATE INDEX IF NOT EXISTS http_sessions_expiry_idx ON http_sessions (expires_on);
CREATE INDEX IF NOT EXISTS http_sessions_key_idx ON http_sessions (key);

COMMIT;

