--
-- COPYRIGHT 2018 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

BEGIN;

CREATE TABLE IF NOT EXISTS appliance_config_store (
    cloud_uuid  uuid REFERENCES appliance_id_map(cloud_uuid) PRIMARY KEY,
    root_hash   bytea NOT NULL,
    ts          timestamp with time zone NOT NULL,
    config      bytea NOT NULL
);

COMMENT ON TABLE appliance_config_store IS 'cloud store for appliance configuration';
COMMENT ON COLUMN appliance_config_store.cloud_uuid IS 'used as the primary key for tracking a system across cloud properties';
COMMENT ON COLUMN appliance_config_store.root_hash IS 'a collision-resistant hash function of the config tree';
COMMENT ON COLUMN appliance_config_store.ts IS 'time of latest config tree modification';
COMMENT ON COLUMN appliance_config_store.config IS 'config tree data blob';

COMMIT;
