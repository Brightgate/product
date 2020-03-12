--
-- COPYRIGHT 2020 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
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
