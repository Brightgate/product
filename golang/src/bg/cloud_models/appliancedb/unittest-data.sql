--
-- COPYRIGHT 2018 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

-- Example Data

INSERT INTO appliance_id_map VALUES (
        '5487addf-9b36-409c-8df8-378d9396b95f'::uuid,
        'DE:AD:BE:EF:F0:0D',
        NULL,
        'peppy-breaker-161717',
        'us-west1',
        'unit-testing',
        'unit-testing-fake-device');

INSERT INTO heartbeat_ingest (cloud_uuid, boot_ts, record_ts) VALUES
    ('5487addf-9b36-409c-8df8-378d9396b95f'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:47+00:00'),
    ('5487addf-9b36-409c-8df8-378d9396b95f'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:53+00:00'),
    ('5487addf-9b36-409c-8df8-378d9396b95f'::uuid, '2017-11-21T01:01:59+00:00', '2017-11-21T01:03:59+00:00');

