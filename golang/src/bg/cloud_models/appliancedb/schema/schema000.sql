--
-- COPYRIGHT 2018 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

DROP TABLE IF EXISTS heartbeat_ingest;
DROP TABLE IF EXISTS identity_map;
DROP TABLE IF EXISTS appliance_id_map;

CREATE TABLE appliance_id_map (
    cloud_uuid            uuid PRIMARY KEY,
    system_repr_mac       varchar(18),
    system_repr_hwserial  varchar(64),
    gcp_project           varchar(32),  -- len determined experimentally
    gcp_region            varchar(32),  -- guessed based on extant regions
    appliance_reg     	  varchar(64),  -- generous sizing, matches IoT Core
    appliance_reg_id      varchar(256)  -- generous sizing, matches IoT Core
);

CREATE UNIQUE INDEX ON appliance_id_map (system_repr_mac);
CREATE UNIQUE INDEX ON appliance_id_map (gcp_project, gcp_region, appliance_reg, appliance_reg_id);

COMMENT ON TABLE appliance_id_map IS 'maps various ways of identifying an appliance to our canonical UUID form';
COMMENT ON COLUMN appliance_id_map.cloud_uuid IS 'used as the primary key for tracking a system across cloud properties';
COMMENT ON COLUMN appliance_id_map.system_repr_mac IS 'representative MAC address';
COMMENT ON COLUMN appliance_id_map.system_repr_hwserial IS 'representative hardware serial number';
COMMENT ON COLUMN appliance_id_map.gcp_project IS 'GCP ProjectId';
COMMENT ON COLUMN appliance_id_map.gcp_region IS 'GCP Region (Location)';
COMMENT ON COLUMN appliance_id_map.appliance_reg IS 'Appliance Registry Name';
COMMENT ON COLUMN appliance_id_map.appliance_reg_id IS 'Appliance Device Name';

CREATE TABLE heartbeat_ingest (
    ingest_id bigserial PRIMARY KEY,
    cloud_uuid uuid REFERENCES appliance_id_map(cloud_uuid) NOT NULL,
    boot_ts timestamp with time zone NOT NULL,
    record_ts timestamp with time zone NOT NULL
);

COMMENT ON TABLE heartbeat_ingest IS 'ingest table for appliance heartbeat (heartbeat and uptime tracking)';
COMMENT ON COLUMN heartbeat_ingest.boot_ts IS 'time system booted';
COMMENT ON COLUMN heartbeat_ingest.record_ts IS 'time recorded on system at heartbeat';
