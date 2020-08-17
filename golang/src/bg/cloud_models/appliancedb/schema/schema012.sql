--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

DROP FUNCTION register_domain(uuid, varchar(16));

CREATE FUNCTION register_domain(u uuid, juris_arg varchar(16))
    RETURNS TABLE(siteid integer, jurisdiction varchar(16), isnew boolean) as $$
DECLARE
    id integer;
BEGIN
    id := next_siteid(juris_arg);
    INSERT INTO site_domains
        (site_uuid, siteid, jurisdiction)
        VALUES (u, id, juris_arg);
    siteid := id;
    jurisdiction := juris_arg;
    isnew := TRUE;
    RETURN NEXT;
    EXCEPTION
    WHEN UNIQUE_VIOLATION THEN
        RETURN QUERY SELECT d.siteid, d.jurisdiction, FALSE
        FROM site_domains d
        WHERE site_uuid = u;
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION register_domain(uuid, varchar(16)) IS 'Function to coordinate insertion into site_domains and update of siteid_sequences';

COMMIT;

