--
-- COPYRIGHT 2018 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

-- The format is inspired by the Google IoT core device registry
CREATE TABLE IF NOT EXISTS appliance_pubkey (
    id                    bigserial PRIMARY KEY,
    cloud_uuid            uuid REFERENCES appliance_id_map(cloud_uuid) NOT NULL,
    format                varchar(32),
    key                   text,
    expiration            timestamp
);

-- n.b. not a *unique* index
CREATE INDEX IF NOT EXISTS appliance_pubkey_cloud_uuid ON appliance_pubkey (cloud_uuid);

COMMENT ON TABLE appliance_pubkey IS 'Holds public keys used to authenticate appliances';
COMMENT ON COLUMN appliance_pubkey.format IS 'Key format (RS256_X509)';
COMMENT ON COLUMN appliance_pubkey.key IS 'Key text representation';
COMMENT ON COLUMN appliance_pubkey.expiration IS 'Optional key expiration date (reserved for future use)';

