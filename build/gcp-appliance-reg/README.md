<!--
Copyright 2020 Brightgate Inc.

This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

This directory contains tools for use with our Google Cloud based Appliance
Registry.  An Appliance Registry tracks vital data about a fleet of appliances,
including the public key information needed to authenticate appliances.

# Appliance Registry

This is a SQL based registry of the Appliances in our fleet; the backend is
Postgres hosted by [Google Cloud SQL](https://cloud.google.com/sql/).

Each Cloud SQL instance can host an arbitrary number of registry databases, so
provisioning a new registry (while probably rare, mostly for testing) is a
lightweight operation. The `new-appliance-reg.sh` script will create a new one.
Once a registry is created, appliances can be provisioned to the
registry using the `new-appliance.sh` script.  Along with the registry, a cloud
pub/sub topic is created for appliance events associated with the registry.

The output of `new-appliance-reg` (besides the database itself) is a shell
include file (and a JSON document) which contains all of the parameters needed
to describe the registry.

Here is an example of registry creation:

```shellsession
$ ./new-appliance-reg.sh
usage: ./new_appliance-reg.sh <credentials-file> <cloudsql-instance> <project-id> <region-id> <registry-id>

$ ./new-appliance-reg.sh ~/secrets/Engineering-f51a19014a36.json eng-scratch0 peppy-breaker-161717 us-west1 testreg3
Postgres database password [user=postgres]:
-------------------------------------------------------------
Creating pubsub topic and database
Activated service account credentials for: [appliance-reg-admin@peppy-breaker-161717.iam.gserviceaccount.com]
Created topic [projects/peppy-breaker-161717/topics/cloudappliance-us-west1-testreg3-events].
Creating Cloud SQL database...done.
Created database [appliance-reg_testreg3].
instance: eng-scratch0
name: appliance-reg_testreg3
project: peppy-breaker-161717
-------------------------------------------------------------
Loading schema files into database
Loading: schema000.sql
NOTICE:  table "heartbeat_ingest" does not exist, skipping
NOTICE:  table "identity_map" does not exist, skipping
NOTICE:  table "appliance_id_map" does not exist, skipping
Loading: schema001.sql
-------------------------------------------------------------
Writing registry info to appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
-------------------------------------------------------------
Created pubsub topic: projects/peppy-breaker-161717/topics/appliance-reg-us-west1-testreg3-events
    Created registry: projects/peppy-breaker-161717/locations/us-west1/registries/testreg3
   CloudSQL instance: eng-scratch0
        Registry URI: postgres:///appliance-reg_testreg3?host=/var/tmp/cloud-sql-sock/peppy-breaker-161717:us-west1:eng-scratch0&user=postgres
      Loaded Schemas: schema000.sql schema001.sql
  Shell include file: appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
      JSON info file: appliance-reg-peppy-breaker-161717-us-west1-testreg3.json
-------------------------------------------------------------
```

Where `Engineering-xxx.json` is the credential fetched from the GCP service
account `appliance-reg-admin@project` in the GCP IAM console.

# Appliance Provisioning

The appliance provisioning process involves creating a key pair, provisioning
the public side of the key pair to the appliance registry, and provisioning the
private side of the key pair to the appliance.  The private side of the key
pair, as well as the rest of the credential parameters, are bundled in a JSON
envelope.  This is done with the `cl-reg` tool, installed in the usual place on
a cloud installation, or in the proto area in a workspace.

Example:

```shellsession
$ cl-reg app add -d output_secrets -i appliance-reg-peppy-breaker-161717-us-west1-testreg3.json test-appliance
Enter DB password:
-------------------------------------------------------------
Created device: projects/peppy-breaker-161717/locations/us-west1/registries/testreg3/appliances/test-appliance
     Site UUID: 7be7369a-fd81-48cb-a5e0-15e9c71fd75d
Appliance UUID: 0fb7871a-ddcd-418e-88ae-f270a4f9b8a6
  Secrets file: output_secrets/test-appliance.cloud.secret.json
-------------------------------------------------------------
Next, provision output_secrets/test-appliance.cloud.secret.json to the appliance at:
    /data/secret/rpcd/cloud.secret.json
    /var/spool/secret/rpcd/cloud.secret.json (on Debian)
```

You can also specify the project, region, and registry name via the environment
variables exported in the `.sh` file created by the registry provisioning
script, or by command-line flags.  By default a site will be auto-provisioned,
but you can override it by passing the `-s` flag.

If you need to provision an appliance on a CloudSQL instance for which there's
not a proxy already running, use the shell script wrapper:

```shellsession
$ . appliance-reg-peppy-breaker-161717-us-west1-testreg3.sh
$ ./new-appliance.sh -c ~/secrets/Engineering-f51a19014a36.json test-appliance
Activated service account credentials for: [cloudappliance-reg-admin@peppy-breaker-161717.iam.gserviceaccount.com]
started sql proxy
Enter DB password:
-------------------------------------------------------------
Created device: projects/peppy-breaker-161717/locations/us-west1/registries/testreg3/appliances/test-appliance
     Site UUID: f9c10fcd-2297-4e28-807e-a6091838f243
Appliance UUID: 007913c9-b3ce-4e11-9e05-5e9effda9a13
  Secrets file: output_secrets/test-appliance.cloud.secret.json
-------------------------------------------------------------
Next, provision output_secrets/test-appliance.cloud.secret.json to the appliance at:
    /data/secret/rpcd/cloud.secret.json
    /var/spool/secret/rpcd/cloud.secret.json (on Debian)
stopped sql proxy
Updated property [core/account].
Revoked credentials:
 - cloudappliance-reg-admin@peppy-breaker-161717.iam.gserviceaccount.com
                  Credentialed Accounts
ACTIVE  ACCOUNT
*       664411676991-compute@developer.gserviceaccount.com
        me@brightgate.com
```

As directed, copy the `.cloud.secret.json` file to the appliance, and
name it `/data/secret/rpcd/cloud.secret.json` (or in `/var/spool` if running on
a Raspberry Pi or out of the proto area on x86.  Restricting the permissions
by running `chmod 600 /data/secret/rpcd/cloud.secret.json` is a good practice.

To confirm that it's working, try:

```shellsession
pi@pi $ sudo /opt/com.brightgate/bin/ap-rpc heartbeat
```
