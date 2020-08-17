--
-- Copyright 2020 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

DO $$
BEGIN
    CREATE ROLE httpd_group WITH NOLOGIN;
    EXCEPTION WHEN duplicate_object THEN
        RAISE NOTICE 'httpd_group role already exists';
END
$$;

GRANT SELECT ON TABLE http_sessions TO httpd_group;
GRANT INSERT ON TABLE http_sessions TO httpd_group;
GRANT UPDATE ON TABLE http_sessions TO httpd_group;
GRANT DELETE ON TABLE http_sessions TO httpd_group;
GRANT USAGE ON SEQUENCE http_sessions_id_seq TO httpd_group;

COMMIT;

