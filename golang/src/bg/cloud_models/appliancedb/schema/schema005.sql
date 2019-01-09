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

CREATE TABLE IF NOT EXISTS customer_site (
  uuid uuid PRIMARY KEY,
  name varchar(63) NOT NULL
  -- XXX Stephen noted that a site might have more than one "name" depending on
  -- who was looking at it.  We could have name and name_managed and call it a
  -- day, or we can make a table of labels, etc, etc.
);
COMMENT ON TABLE customer_site IS 'used as the primary key for tracking a site across cloud properties';
COMMENT ON COLUMN customer_site.name IS 'Name for the site useful for humans';

INSERT INTO customer_site (uuid, name) VALUES
  ('00000000-0000-0000-0000-000000000000'::uuid, 'sentinel:null-site')
  ON CONFLICT DO NOTHING;

ALTER TABLE appliance_id_map ADD COLUMN site_uuid uuid REFERENCES customer_site (uuid);
COMMENT ON COLUMN appliance_id_map.site_uuid IS 'The site to which the appliance belongs';

ALTER TABLE appliance_id_map RENAME COLUMN cloud_uuid TO appliance_uuid;

-- We do a very simple migration operation here for existing databases which are
-- acquiring the customer_site table: all existing appliances in the registry are
-- presumed to be sites.  Because there are various other properties (gcp storage,
-- etc) which use these UUID, it's too painful to change them everywhere.  So
-- force the site and appliance UUIDs to be the same for this population of
-- appliances.
INSERT INTO customer_site (uuid, name)
SELECT appliance_uuid, appliance_reg_id FROM appliance_id_map WHERE site_uuid IS NULL;

UPDATE appliance_id_map
  SET site_uuid = appliance_uuid WHERE site_uuid IS NULL;
ALTER TABLE appliance_id_map ALTER COLUMN site_uuid SET NOT NULL;

ALTER TABLE heartbeat_ingest DROP CONSTRAINT heartbeat_ingest_cloud_uuid_fkey;
ALTER TABLE heartbeat_ingest RENAME COLUMN cloud_uuid to site_uuid;
ALTER TABLE heartbeat_ingest ADD CONSTRAINT heartbeat_ingest_site_uuid_fkey FOREIGN KEY (site_uuid) REFERENCES customer_site (uuid);
COMMENT ON COLUMN heartbeat_ingest.site_uuid IS 'used as the primary key for tracking a site across cloud properties';
ALTER TABLE heartbeat_ingest RENAME TO site_heartbeat_ingest;

ALTER TABLE appliance_cloudstorage DROP CONSTRAINT appliance_cloudstorage_cloud_uuid_fkey;
ALTER TABLE appliance_cloudstorage RENAME COLUMN cloud_uuid to site_uuid;
ALTER TABLE appliance_cloudstorage ADD CONSTRAINT appliance_cloudstorage_site_uuid_fkey FOREIGN KEY (site_uuid) REFERENCES customer_site (uuid);
COMMENT ON COLUMN appliance_cloudstorage.site_uuid IS 'used as the primary key for tracking a site across cloud properties';
ALTER TABLE appliance_cloudstorage RENAME TO site_cloudstorage;

ALTER TABLE appliance_config_store DROP CONSTRAINT appliance_config_store_cloud_uuid_fkey;
ALTER TABLE appliance_config_store RENAME COLUMN cloud_uuid to site_uuid;
ALTER TABLE appliance_config_store ADD CONSTRAINT appliance_config_store_site_uuid_fkey FOREIGN KEY (site_uuid) REFERENCES customer_site (uuid);
COMMENT ON COLUMN appliance_config_store.site_uuid IS 'used as the primary key for tracking a site across cloud properties';
ALTER TABLE appliance_config_store RENAME TO site_config_store;

ALTER TABLE appliance_commands DROP CONSTRAINT appliance_commands_cloud_uuid_fkey;
ALTER TABLE appliance_commands RENAME COLUMN cloud_uuid to site_uuid;
ALTER TABLE appliance_commands ADD CONSTRAINT appliance_commands_site_uuid_fkey FOREIGN KEY (site_uuid) REFERENCES customer_site (uuid);
COMMENT ON COLUMN appliance_commands.site_uuid IS 'used as the primary key for tracking a site across cloud properties';
ALTER TABLE appliance_commands RENAME TO site_commands;

-- appliance_pubkey is the remaining table with a cloud_uuid fkey-- fix it up
ALTER TABLE appliance_pubkey RENAME COLUMN cloud_uuid TO appliance_uuid;
ALTER TABLE appliance_pubkey RENAME CONSTRAINT appliance_pubkey_cloud_uuid_fkey TO appliance_pubkey_appliance_uuid_fkey;
ALTER INDEX appliance_pubkey_cloud_uuid RENAME TO appliance_pubkey_appliance_uuid;

COMMIT;
