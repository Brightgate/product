--
-- COPYRIGHT 2020 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

INSERT INTO releases (release_uuid, metadata) VALUES
    ('00000000-0000-0000-0000-000000000000'::uuid, '{"name": "Unknown/Mixed"}'::json)
    ON CONFLICT DO NOTHING;
