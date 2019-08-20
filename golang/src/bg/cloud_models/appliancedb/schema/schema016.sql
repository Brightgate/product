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

CREATE TABLE IF NOT EXISTS platforms (
    name             text PRIMARY KEY
);
COMMENT ON TABLE platforms IS 'Hardware platforms';
COMMENT ON COLUMN platforms.name IS 'Platform name';

INSERT INTO platforms (name) VALUES
    ('mt7623'),
    ('rpi3'),
    ('x86')
    ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS repository_abbreviations (
    abbrev           text PRIMARY KEY
);
COMMENT ON TABLE repository_abbreviations IS 'Abbreviations for the source code repository names';
COMMENT ON COLUMN repository_abbreviations.abbrev IS 'Abbreviation text';

INSERT INTO repository_abbreviations (abbrev) VALUES
    ('PS'),
    ('XS'),
    ('WRT'),
    ('VUB')
    ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS hash_types (
    name             text PRIMARY KEY
);
COMMENT ON TABLE hash_types IS 'Names of hash types used to checksum artifacts';
COMMENT ON COLUMN hash_types.name IS 'Name of has type';

INSERT INTO hash_types (name) VALUES
    ('SHA256')
    ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS artifacts (
    artifact_uuid    uuid PRIMARY KEY,
    platform_name    text REFERENCES platforms(name) NOT NULL,
    repo_name        text REFERENCES repository_abbreviations(abbrev) NOT NULL,
    commit_hash      bytea NOT NULL,
    generation       int NOT NULL,
    filename         text NOT NULL,
    hash             bytea NOT NULL,
    hash_type        text REFERENCES hash_types(name) NOT NULL,
    UNIQUE (platform_name, repo_name, commit_hash, generation, filename)
);
COMMENT ON TABLE artifacts IS 'Linkage of platform and commit to an artifact of that build';
COMMENT ON COLUMN artifacts.artifact_uuid IS 'Artifact UUID';
COMMENT ON COLUMN artifacts.platform_name IS 'Platform name';
COMMENT ON COLUMN artifacts.repo_name IS 'Name of repository where commit resulting in artifact is found';
COMMENT ON COLUMN artifacts.commit_hash IS 'Hash of commit in repository';
COMMENT ON COLUMN artifacts.generation IS 'Incremented if a commit is rebuilt';
COMMENT ON COLUMN artifacts.hash IS 'Hash of artifact bytes';
COMMENT ON COLUMN artifacts.hash_type IS 'Type of hash used in hash column';

CREATE TABLE IF NOT EXISTS releases (
    release_uuid     uuid PRIMARY KEY,
    create_ts        timestamp with time zone NOT NULL DEFAULT now(),
    metadata         jsonb NOT NULL
);
CREATE INDEX IF NOT EXISTS releases_metadata_gin ON releases USING gin (metadata jsonb_path_ops);
COMMENT ON TABLE releases IS 'Metadata for releases';
COMMENT ON COLUMN releases.release_uuid IS 'Release UUID';
COMMENT ON COLUMN releases.create_ts IS 'Time when the release was created';
COMMENT ON COLUMN releases.metadata IS 'Metadata for release; should be limited to string key/value pairs';

CREATE TABLE IF NOT EXISTS release_artifacts (
    id               bigserial PRIMARY KEY,
    release_uuid     uuid REFERENCES releases(release_uuid) NOT NULL,
    artifact_uuid    uuid REFERENCES artifacts(artifact_uuid) NOT NULL,
    UNIQUE (artifact_uuid, release_uuid)
);
CREATE INDEX ON release_artifacts (release_uuid);
COMMENT ON TABLE release_artifacts IS 'Map of releases to the artifacts comprising them';
COMMENT ON COLUMN release_artifacts.release_uuid IS 'Release UUID';
COMMENT ON COLUMN release_artifacts.artifact_uuid IS 'Artifact UUID';

CREATE TABLE IF NOT EXISTS appliance_release_history (
    appliance_uuid       uuid REFERENCES appliance_id_map(appliance_uuid),
    release_uuid         uuid REFERENCES releases(release_uuid) NOT NULL,
    updated_ts           timestamp with time zone NOT NULL DEFAULT now(),
    UNIQUE (appliance_uuid, release_uuid)
);
COMMENT ON TABLE appliance_release_history IS 'Links appliances with current and historical release information';
COMMENT ON COLUMN appliance_release_history.appliance_uuid IS 'UUID of the appliance';
COMMENT ON COLUMN appliance_release_history.release_uuid IS 'Release we think the appliance is or was running';
COMMENT ON COLUMN appliance_release_history.updated_ts IS 'Time when the appliance first reported running this release';

CREATE TABLE IF NOT EXISTS appliance_release_targets (
    appliance_uuid       uuid REFERENCES appliance_id_map(appliance_uuid) PRIMARY KEY,
    release_uuid         uuid REFERENCES releases(release_uuid)
);
COMMENT ON TABLE appliance_release_targets IS 'Links appliances to their expected release';
COMMENT ON COLUMN appliance_release_targets.appliance_uuid IS 'UUID of the appliance';
COMMENT ON COLUMN appliance_release_targets.release_uuid IS 'Release the appliance is expected to upgrade to';

COMMIT;
