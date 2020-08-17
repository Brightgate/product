--
-- Copyright 2018 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

-- The format is inspired by the Google IoT core device registry
CREATE TABLE IF NOT EXISTS appliance_pubkey (
    id                    bigserial PRIMARY KEY,
    cloud_uuid            uuid REFERENCES appliance_id_map(cloud_uuid) NOT NULL,
    format                varchar(32),
    key                   text,
    expiration            timestamp with time zone
);

-- n.b. not a *unique* index
CREATE INDEX IF NOT EXISTS appliance_pubkey_cloud_uuid ON appliance_pubkey (cloud_uuid);

COMMENT ON TABLE appliance_pubkey IS 'Holds public keys used to authenticate appliances';
COMMENT ON COLUMN appliance_pubkey.format IS 'Key format (RS256_X509)';
COMMENT ON COLUMN appliance_pubkey.key IS 'Key text representation';
COMMENT ON COLUMN appliance_pubkey.expiration IS 'Optional key expiration date (reserved for future use)';

COMMIT;

