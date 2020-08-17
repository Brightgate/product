--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TABLE IF NOT EXISTS jurisdictions (
    name             varchar(16) PRIMARY KEY
);
COMMENT ON TABLE jurisdictions IS 'list of jurisdictions';
COMMENT ON COLUMN jurisdictions.name IS 'name of the jurisdiction';

CREATE TABLE IF NOT EXISTS siteid_sequences (
    jurisdiction     varchar(16) REFERENCES jurisdictions(name) PRIMARY KEY,
    factor           integer DEFAULT 50207,
    constant         integer DEFAULT 2777,
    range_min        integer DEFAULT 10000,
    range_max        integer DEFAULT 100000,
    max_claimed      integer,
    max_unclaimed    integer
);

COMMENT ON TABLE siteid_sequences IS 'maximum claimed siteids';
COMMENT ON COLUMN siteid_sequences.jurisdiction IS 'name of the jurisdiction';
COMMENT ON COLUMN siteid_sequences.max_claimed IS 'maximum claimed siteid for the jurisdiction';
COMMENT ON COLUMN siteid_sequences.max_unclaimed IS 'maximum unclaimed siteid for the jurisdiction';


CREATE TABLE IF NOT EXISTS site_domains (
    site_uuid        uuid REFERENCES customer_site(uuid) PRIMARY KEY,
    siteid           integer,
    jurisdiction     varchar(16) REFERENCES jurisdictions(name),
    UNIQUE (siteid, jurisdiction)
);

COMMENT ON TABLE site_domains IS 'mapping between site UUIDs and domains for TLS certificates';
COMMENT ON COLUMN site_domains.site_uuid IS 'used as the primary key for tracking a site across cloud properties';
COMMENT ON COLUMN site_domains.siteid IS 'the raw per-jurisdiction subdomain';
COMMENT ON COLUMN site_domains.jurisdiction IS 'the jurisdiction portion of the domain';


CREATE TABLE IF NOT EXISTS site_certs (
    siteid           integer,
    jurisdiction     varchar(16) REFERENCES jurisdictions(name),
    fingerprint      bytea NOT NULL,
    expiration       timestamp with time zone,
    cert             bytea NOT NULL,
    issuercert       bytea NOT NULL,
    key              bytea NOT NULL,
    PRIMARY KEY (siteid, jurisdiction, fingerprint)
);

COMMENT ON TABLE site_certs IS 'TLS key/certificate material';
COMMENT ON COLUMN site_certs.siteid IS 'the site ID of the domain of the certificate';
COMMENT ON COLUMN site_certs.jurisdiction IS 'the jurisdiction of the domain of the certificate';
COMMENT ON COLUMN site_certs.fingerprint IS 'SHA-1 fingerprint of the certificate';
COMMENT ON COLUMN site_certs.expiration IS 'the NotAfter date of the certificate';
COMMENT ON COLUMN site_certs.cert IS 'the raw bytes of the certificate';
COMMENT ON COLUMN site_certs.issuercert IS 'the raw bytes of the issuer certificate';
COMMENT ON COLUMN site_certs.key IS 'the raw bytes of the private key';


CREATE TABLE IF NOT EXISTS failed_domains (
    siteid           integer NOT NULL,
    jurisdiction     varchar(16) REFERENCES jurisdictions(name),
    PRIMARY KEY (siteid, jurisdiction)
);

COMMENT ON TABLE failed_domains IS 'domains whose ACME verification failed';
COMMENT ON COLUMN failed_domains.siteid IS 'siteid of failed domain';
COMMENT ON COLUMN failed_domains.jurisdiction IS 'jurisidction of failed domain';


CREATE FUNCTION obfuscate_siteid(siteid integer, juris_arg text) RETURNS integer as $$
DECLARE
    c record;
BEGIN
    SELECT factor, constant, range_max, range_min
    INTO c
    FROM siteid_sequences
    WHERE jurisdiction = juris_arg;
    RETURN (c.factor * siteid + c.constant) % (c.range_max - c.range_min) + c.range_min;
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION obfuscate_siteid(integer, text) IS 'maps consecutive siteids onto another sequence';

-- Note this is *not* the same as the Go function DataStore.ComputeDomain(),
-- which returns the complete DNS domain name.  This is just a convenience
-- function for people poking at the database commandline.
CREATE FUNCTION compute_domain(siteid integer, jurisdiction text) RETURNS text as $$
BEGIN
    RETURN concat_ws('.', obfuscate_siteid(siteid, jurisdiction), NULLIF(jurisdiction, ''));
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION compute_domain(integer, text) IS 'computes the domain based on siteid and jurisdiction';

CREATE FUNCTION next_siteid(juris_arg text) RETURNS integer as $$
DECLARE
    nextval integer;
BEGIN
    UPDATE siteid_sequences
        SET max_claimed = COALESCE(max_claimed, -1) + 1
        WHERE jurisdiction = juris_arg
        RETURNING max_claimed
        INTO STRICT nextval;
    RETURN nextval;
    EXCEPTION
        WHEN NO_DATA_FOUND THEN
            INSERT INTO jurisdictions (name) VALUES (juris_arg) ON CONFLICT DO NOTHING;
            INSERT INTO siteid_sequences (jurisdiction, max_claimed) VALUES (juris_arg, -1);
            RETURN next_siteid(juris_arg);
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION next_siteid(text) IS 'increments and returns the maximum claimed siteid for the given jurisdiction';

-- Identical to next_siteid(), except that it operates on max_unclaimed.
CREATE FUNCTION next_siteid_unclaimed(juris_arg text) RETURNS integer as $$
DECLARE
    nextval integer;
BEGIN
    UPDATE siteid_sequences
        SET max_unclaimed = COALESCE(max_unclaimed, -1) + 1
        WHERE jurisdiction = juris_arg
        RETURNING max_unclaimed
        INTO STRICT nextval;
    RETURN nextval;
    EXCEPTION
        WHEN NO_DATA_FOUND THEN
            INSERT INTO jurisdictions (name) VALUES (juris_arg) ON CONFLICT DO NOTHING;
            INSERT INTO siteid_sequences (jurisdiction, max_unclaimed) VALUES (juris_arg, -1);
            RETURN next_siteid_unclaimed(juris_arg);
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION next_siteid_unclaimed(text) IS 'increments and returns the maximum unclaimed siteid for the given jurisdiction';

CREATE FUNCTION default_siteid() RETURNS trigger as $$
BEGIN
    NEW.siteid := next_siteid(NEW.jurisdiction);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
COMMENT ON FUNCTION default_siteid() IS 'computes the default value for the siteid column in site_domains';

CREATE TRIGGER default_siteid BEFORE INSERT ON site_domains FOR EACH ROW EXECUTE PROCEDURE default_siteid();

COMMIT;

