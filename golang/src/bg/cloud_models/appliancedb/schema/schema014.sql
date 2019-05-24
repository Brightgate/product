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

CREATE TABLE site_net_exception (
	id bigserial PRIMARY KEY,
	site_uuid uuid REFERENCES customer_site(uuid) NOT NULL,
	ts timestamp with time zone NOT NULL,
	reason text,
	macaddr bigint,
	exc jsonb NOT NULL
);

CREATE INDEX ON site_net_exception (site_uuid);

COMMIT;
