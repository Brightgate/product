--
-- COPYRIGHT 2019 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

-- Example Data

INSERT INTO customer_site (uuid, name) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, 'site1'),
    ('10000002-0002-0002-0002-000000000002'::uuid, 'site2');

INSERT INTO appliance_id_map (appliance_uuid, site_uuid, system_repr_mac, system_repr_hwserial, gcp_project, gcp_region, appliance_reg, appliance_reg_id) VALUES
    ('00000001-0001-0001-0001-000000000001'::uuid, '10000001-0001-0001-0001-000000000001'::uuid, 'DE:AD:BE:EF:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance1'),
    ('00000002-0002-0002-0002-000000000002'::uuid, '10000002-0002-0002-0002-000000000002'::uuid, 'FE:ED:FA:CE:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance2');

INSERT INTO site_heartbeat_ingest (site_uuid, boot_ts, record_ts) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:47+00:00'),
    ('10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:53+00:00'),
    ('10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:59+00:00');

INSERT INTO appliance_pubkey (appliance_uuid, format, key, expiration) VALUES
    ('00000001-0001-0001-0001-000000000001'::uuid, 'RS256_X509', 'pemdata1', '2017-11-21T01:03:59+00:00'),
    ('00000001-0001-0001-0001-000000000001'::uuid, 'RS256_X509', 'pemdata2', '2017-11-21T01:03:59+00:00');

INSERT INTO site_cloudstorage (site_uuid, bucket, provider) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, 'bg-appliance-data-00000001-0001-0001-0001-000000000001', 'gcs');

INSERT INTO site_config_store (site_uuid, root_hash, ts, config) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '\xDEADBEEF', '2017-11-21T01:03:59+00:00', '\xDEADBEEF');

INSERT INTO site_commands (site_uuid, enq_ts, config_query) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:03:59+00:00', '\xDEADBEEF');
