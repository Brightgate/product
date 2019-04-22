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
