--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


-- Example Data
INSERT INTO organization (uuid, name) VALUES
    ('30000000-3000-3000-3000-000000000001'::uuid, 'org1'),
    ('30000000-3000-3000-3000-000000000002'::uuid, 'org2');

INSERT INTO customer_site (uuid, name, organization_uuid) VALUES
    ('20000000-2000-2000-2000-000000000001'::uuid, 'site1', '30000000-3000-3000-3000-000000000001'::uuid),
    ('20000000-2000-2000-2000-000000000002'::uuid, 'site2', '30000000-3000-3000-3000-000000000002'::uuid);

INSERT INTO appliance_id_map (appliance_uuid, site_uuid, system_repr_mac, system_repr_hwserial, gcp_project, gcp_region, appliance_reg, appliance_reg_id) VALUES
    ('10000000-1000-1000-1000-000000000001'::uuid, '20000000-2000-2000-2000-000000000001'::uuid, 'DE:AD:BE:EF:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance1'),
    ('10000000-1000-1000-1000-000000000002'::uuid, '20000000-2000-2000-2000-000000000002'::uuid, 'FE:ED:FA:CE:F0:0D', NULL, 'test-project', 'test-region', 'test-registry', 'test-appliance2');

INSERT INTO heartbeat_ingest (appliance_uuid, site_uuid, boot_ts, record_ts) VALUES
    ('10000000-1000-1000-1000-000000000001'::uuid, '20000000-2000-2000-2000-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:47+00:00'),
    ('10000000-1000-1000-1000-000000000001'::uuid, '20000000-2000-2000-2000-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:53+00:00'),
    ('10000000-1000-1000-1000-000000000001'::uuid, '20000000-2000-2000-2000-000000000001'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:59+00:00');

INSERT INTO appliance_pubkey (appliance_uuid, format, key, expiration) VALUES
    ('10000000-1000-1000-1000-000000000001'::uuid, 'RS256_X509', 'pemdata1', '2017-11-21T01:03:59+00:00'),
    ('10000000-1000-1000-1000-000000000001'::uuid, 'RS256_X509', 'pemdata2', '2017-11-21T01:03:59+00:00');

INSERT INTO site_cloudstorage (site_uuid, bucket, provider) VALUES
    ('20000000-2000-2000-2000-000000000001'::uuid, 'bg-appliance-data-xxx', 'gcs');

INSERT INTO site_config_store (site_uuid, root_hash, ts, config) VALUES
    ('20000000-2000-2000-2000-000000000001'::uuid, '\xDEADBEEF', '2017-11-21T01:03:59+00:00', '\xDEADBEEF');

INSERT INTO site_commands (site_uuid, enq_ts, config_query) VALUES
    ('20000000-2000-2000-2000-000000000001'::uuid, '2017-11-21T01:03:59+00:00', '\xDEADBEEF');

INSERT INTO site_net_exception (site_uuid, ts, reason, macaddr, exc) VALUES
    ('20000000-2000-2000-2000-000000000001'::uuid, '2019-05-22 21:17:52.469775+00', 'TEST_EXCEPTION', 18838586676582,
        '{"reason": "TEST_EXCEPTION", "details": ["detail 1", "detail 2"], "message": "This is a test of the emergency broadcast system.", "protocol": "IP", "timestamp": "2019-05-22T21:17:52.469774773Z", "virtualAP": "psk", "macAddress": "18838586676582", "ipv4Address": 2864434397}')

