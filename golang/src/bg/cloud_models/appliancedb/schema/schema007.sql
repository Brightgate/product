--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TABLE IF NOT EXISTS account_secrets (
	account_uuid uuid REFERENCES account (uuid) PRIMARY KEY NOT NULL,
	appliance_user_bcrypt text,
	appliance_user_mschapv2 text
);

COMMENT ON TABLE account_secrets IS 'User account data with high sensitivity';
COMMENT ON COLUMN account_secrets.appliance_user_bcrypt IS 'bcrypt user password; client supplies encryption';
COMMENT ON COLUMN account_secrets.appliance_user_mschapv2 IS 'mschapv2 user password; client supplies encryption';

COMMIT;

