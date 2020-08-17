--
-- Copyright 2018 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

