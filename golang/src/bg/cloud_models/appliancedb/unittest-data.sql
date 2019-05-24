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
INSERT INTO organization (uuid, name) VALUES
    ('20000001-0001-0001-0001-000000000001'::uuid, 'org1'),
    ('20000002-0002-0002-0002-000000000002'::uuid, 'org2');

INSERT INTO customer_site (uuid, name, organization_uuid) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, 'site1', '20000001-0001-0001-0001-000000000001'::uuid),
    ('10000002-0002-0002-0002-000000000002'::uuid, 'site2', '20000002-0002-0002-0002-000000000002'::uuid);

INSERT INTO appliance_id_map (appliance_uuid, site_uuid, system_repr_mac, system_repr_hwserial, gcp_project, gcp_region, appliance_reg, appliance_reg_id) VALUES
    ('00000001-0001-0001-0001-000000000001'::uuid, '10000001-0001-0001-0001-000000000001'::uuid, 'DE:AD:BE:EF:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance1'),
    ('00000002-0002-0002-0002-000000000002'::uuid, '10000002-0002-0002-0002-000000000002'::uuid, 'FE:ED:FA:CE:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance2');

INSERT INTO heartbeat_ingest (appliance_uuid, site_uuid, boot_ts, record_ts) VALUES
    ('00000001-0001-0001-0001-000000000001'::uuid, '10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:47+00:00'),
    ('00000001-0001-0001-0001-000000000001'::uuid, '10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:53+00:00'),
    ('00000001-0001-0001-0001-000000000001'::uuid, '10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:59+00:00');

INSERT INTO appliance_pubkey (appliance_uuid, format, key, expiration) VALUES
    ('00000001-0001-0001-0001-000000000001'::uuid, 'RS256_X509', 'pemdata1', '2017-11-21T01:03:59+00:00'),
    ('00000001-0001-0001-0001-000000000001'::uuid, 'RS256_X509', 'pemdata2', '2017-11-21T01:03:59+00:00');

INSERT INTO site_cloudstorage (site_uuid, bucket, provider) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, 'bg-appliance-data-00000001-0001-0001-0001-000000000001', 'gcs');

INSERT INTO site_config_store (site_uuid, root_hash, ts, config) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '\xDEADBEEF', '2017-11-21T01:03:59+00:00', '\xDEADBEEF');

INSERT INTO site_commands (site_uuid, enq_ts, config_query) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '2017-11-21T01:03:59+00:00', '\xDEADBEEF');

INSERT INTO site_net_exception (site_uuid, ts, reason, macaddr, exc) VALUES
    ('10000001-0001-0001-0001-000000000001'::uuid, '2019-05-22 21:17:52.469775+00', 'TEST_EXCEPTION', 18838586676582,
        '{"reason": "TEST_EXCEPTION", "details": ["detail 1", "detail 2"], "message": "This is a test of the emergency broadcast system.", "protocol": "IP", "timestamp": "2019-05-22T21:17:52.469774773Z", "virtualAP": "psk", "macAddress": "18838586676582", "ipv4Address": 2864434397}')
