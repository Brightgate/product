--
-- Copyright 2018 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TABLE IF NOT EXISTS appliance_commands (
    id               bigserial PRIMARY KEY,
    cloud_uuid       uuid REFERENCES appliance_id_map(cloud_uuid),
    enq_ts           timestamp with time zone NOT NULL DEFAULT now(),
    sent_ts          timestamp with time zone,
    resent_n         integer,
    done_ts          timestamp with time zone,
    state            char(4) CHECK (state IN ('ENQD','WORK','CNCL','DONE')) NOT NULL DEFAULT 'ENQD',
    config_query     bytea NOT NULL,
    config_response  bytea
);

-- Create a partial index for fetch to allow fast access to just those commands
-- which are enqueued or already working (in case fetch needs to re-fetch).
CREATE INDEX IF NOT EXISTS appliance_commands_fetch_idx
    ON appliance_commands (cloud_uuid)
    WHERE (state = 'ENQD' OR state = 'WORK');

COMMENT ON TABLE appliance_commands IS 'appliance command queue';
COMMENT ON COLUMN appliance_commands.cloud_uuid IS 'used as the primary key for tracking a system across cloud properties';
COMMENT ON COLUMN appliance_commands.enq_ts IS 'time the command was posted to the queue';
COMMENT ON COLUMN appliance_commands.sent_ts IS 'time the command was last fetched and sent to the appliance';
COMMENT ON COLUMN appliance_commands.resent_n IS 'number of times the command was re-fetched by the appliance';
COMMENT ON COLUMN appliance_commands.done_ts IS 'time a response was received';
COMMENT ON COLUMN appliance_commands.state IS 'state of the command';
COMMENT ON COLUMN appliance_commands.config_query IS 'configuration query blob';
COMMENT ON COLUMN appliance_commands.config_response IS 'configuration response blob';
COMMENT ON INDEX appliance_commands_fetch_idx IS 'Partial index for fetchable commands';

COMMIT;

