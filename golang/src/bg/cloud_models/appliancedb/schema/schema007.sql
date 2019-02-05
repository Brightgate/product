--
-- COPYRIGHT 2019 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
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
