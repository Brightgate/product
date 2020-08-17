--
-- Copyright 2019 Brightgate Inc.
--
-- This Source Code Form is subject to the terms of the Mozilla Public
-- License, v. 2.0. If a copy of the MPL was not distributed with this
-- file, You can obtain one at https://mozilla.org/MPL/2.0/.
--


BEGIN;

CREATE TABLE IF NOT EXISTS ao_role (
  name varchar(64) PRIMARY KEY
);
COMMENT ON TABLE ao_role IS 'The list of valid account/org roles';
COMMENT ON COLUMN ao_role.name IS 'Role name';

INSERT INTO ao_role (name) VALUES
  ('admin'),
  ('user')
  ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS account_org_role (
  account_uuid uuid REFERENCES account (uuid),
  organization_uuid uuid REFERENCES organization (uuid),
  role varchar(64) REFERENCES ao_role (name),
  PRIMARY KEY (account_uuid, organization_uuid, role)
);
COMMENT ON TABLE account_org_role IS 'Roles, defined as account,organization,rolename tuples';
COMMENT ON COLUMN account_org_role.account_uuid IS 'Account the role applies to';
COMMENT ON COLUMN account_org_role.organization_uuid IS 'Organization the role applies to';
COMMENT ON COLUMN account_org_role.role IS 'Role name';

COMMIT;

