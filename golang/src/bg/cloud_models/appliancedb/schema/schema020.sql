--
-- Copyright 2020 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


INSERT INTO releases (release_uuid, metadata) VALUES
    ('00000000-0000-0000-0000-000000000000'::uuid, '{"name": "Unknown/Mixed"}'::json)
    ON CONFLICT DO NOTHING;

