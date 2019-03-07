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
