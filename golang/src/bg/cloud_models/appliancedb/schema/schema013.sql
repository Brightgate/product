
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

-- heartbeat_ingest is created from the site_heartbeat_ingest table, which is
-- then dropped.  The main distinction is that the heartbeat_ingest table is
-- aware that all appliances at a site can now send heartbeats.  In the case
-- where there are multiple appliances for a site, we arbitrarily assign all
-- old heartbeats to the lower uuid'd appliance; we have no other simple way to
-- resolve the conflict.
CREATE TABLE heartbeat_ingest (
	ingest_id bigserial PRIMARY KEY,
	appliance_uuid uuid REFERENCES appliance_id_map(appliance_uuid) NOT NULL,
	site_uuid uuid REFERENCES customer_site(uuid) NOT NULL,
	boot_ts timestamp with time zone NOT NULL,
	record_ts timestamp with time zone NOT NULL
);

-- Join the heartbeat table with appliance_id_map in order to have
-- something to fill into the appliance_uuid field in the new table.
WITH q AS (
	SELECT DISTINCT ON (site_heartbeat_ingest.ingest_id) site_heartbeat_ingest.ingest_id,
	appliance_id_map.appliance_uuid as appliance_uuid,
	site_heartbeat_ingest.site_uuid as site_uuid,
	site_heartbeat_ingest.boot_ts as boot_ts,
	site_heartbeat_ingest.record_ts as record_ts
FROM appliance_id_map, site_heartbeat_ingest
WHERE
	appliance_id_map.site_uuid = site_heartbeat_ingest.site_uuid
ORDER BY
	-- Must order by ingest_id first; but then we need another tiebreaker.
	-- We sort by appliance_uuid, which is arbitrary but deterministic.
	site_heartbeat_ingest.ingest_id, appliance_id_map.appliance_uuid
) INSERT INTO heartbeat_ingest
	(appliance_uuid, site_uuid, boot_ts, record_ts)
	SELECT appliance_uuid, site_uuid, boot_ts, record_ts FROM q;

DROP TABLE site_heartbeat_ingest;

COMMIT;
