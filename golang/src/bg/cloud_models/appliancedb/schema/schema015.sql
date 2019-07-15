--
-- COPYRIGHT 2019 Brightgate Inc. All rights reserved.
--
-- This copyright notice is Copyright Management Information under 17 USC 1202
-- and is included to protect this work and deter copyright infringement.
-- Removal or alteration of this Copyright Management Information without the
-- express written permission of Brightgate Inc is prohibited, and any
-- such unauthorized removal or alteration will be a violation of federal law.
--

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

BEGIN;

CREATE TABLE IF NOT EXISTS relationship (
    name varchar(64) PRIMARY KEY
);
COMMENT ON TABLE relationship is 'Valid relationship names';

INSERT INTO relationship VALUES ('self'), ('msp');

CREATE TABLE IF NOT EXISTS relationship_roles (
    relationship varchar(64) REFERENCES relationship (name),
    role varchar(64) REFERENCES ao_role
);
COMMENT ON TABLE relationship_roles IS 'Maps relationships to the limit set of roles which can be granted by the relationship';

INSERT INTO relationship_roles VALUES
    ('self', 'admin'),
    ('self', 'user'),
    ('msp', 'admin'),
    ('msp', 'user');

CREATE TABLE IF NOT EXISTS org_org_relationship (
    uuid uuid PRIMARY KEY,
    organization_uuid uuid REFERENCES organization (uuid),
    target_organization_uuid uuid REFERENCES organization (uuid),
    relationship varchar(64) REFERENCES relationship (name),
    UNIQUE (organization_uuid, target_organization_uuid, relationship),
    -- enforce the special rules of the 'self' relationship
    CHECK
      ((organization_uuid = target_organization_uuid AND relationship = 'self') OR
       (organization_uuid != target_organization_uuid AND relationship != 'self'))
);
COMMENT ON TABLE org_org_relationship IS 'Describes relationships between organizations';
COMMENT ON COLUMN org_org_relationship.organization_uuid IS 'The originator of the relationship';
COMMENT ON COLUMN org_org_relationship.target_organization_uuid IS 'The target of the relationship';
COMMENT ON COLUMN org_org_relationship.relationship IS 'The type of relationship';

-- Establish the 'self' relationship for all existing organizations
INSERT INTO org_org_relationship (uuid, organization_uuid, target_organization_uuid, relationship)
    SELECT uuid_generate_v4(), organization.uuid, organization.uuid, 'self'
    FROM organization
    WHERE
      organization.uuid != '00000000-0000-0000-0000-000000000000';

ALTER TABLE account_org_role RENAME TO account_org_role_old;

CREATE TABLE account_org_role (
    account_uuid uuid REFERENCES account (uuid),
    organization_uuid uuid, -- see FK below
    target_organization_uuid uuid, -- see FK below
    relationship varchar(64), -- see FK below
    role varchar(64) REFERENCES ao_role (name),
    FOREIGN KEY (organization_uuid, target_organization_uuid, relationship)
      REFERENCES org_org_relationship (organization_uuid, target_organization_uuid, relationship),
    PRIMARY KEY (account_uuid, target_organization_uuid, relationship, role)
);

-- Migrate old account_org_role entries over to new 'self' relationship roles.
INSERT INTO account_org_role (account_uuid, organization_uuid, target_organization_uuid, relationship, role)
    SELECT
      account_org_role_old.account_uuid,
      org_org_relationship.organization_uuid,
      org_org_relationship.target_organization_uuid,
      'self',
      account_org_role_old.role
    FROM account_org_role_old, org_org_relationship
    WHERE
        account_org_role_old.organization_uuid = org_org_relationship.organization_uuid AND
        org_org_relationship.relationship = 'self';

DROP TABLE account_org_role_old;

COMMIT;
