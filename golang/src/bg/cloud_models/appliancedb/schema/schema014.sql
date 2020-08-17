--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
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

