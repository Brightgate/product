/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"

	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
)

func listAppliances(cmd *cobra.Command, args []string) error {
	appID, _ := cmd.Flags().GetString("name")
	orgs, _ := cmd.Flags().GetStringSlice("org")
	sites, _ := cmd.Flags().GetStringSlice("site")

	if len(orgs) > 0 && len(sites) > 0 {
		return fmt.Errorf("Only one of --org and --site may be specified")
	}

	db, reg, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	ctx := context.Background()

	var apps []appliancedb.ApplianceID
	for _, site := range sites {
		fm, err := registry.SiteUUIDByNameFuzzy(ctx, db, site)
		if err != nil {
			if ase, ok := err.(registry.AmbiguousSiteError); ok {
				return errors.New(strings.TrimSpace(ase.Pretty()))
			}
			return err
		}
		if fm.Name != "" {
			fmt.Fprintf(os.Stderr,
				"%q matched more than one site, but %q (%s) "+
					"seemed the most likely\n",
				site, fm.Name, fm.UUID)
		}
		a, err := db.ApplianceIDsBySiteID(ctx, fm.UUID)
		if err != nil {
			return err
		}
		apps = append(apps, a...)
	}

	for _, org := range orgs {
		fm, err := registry.OrgUUIDByNameFuzzy(ctx, db, org)
		if err != nil {
			if aoe, ok := err.(registry.AmbiguousOrgError); ok {
				return errors.New(strings.TrimSpace(aoe.Pretty()))
			}
			return err
		}
		if fm.Name != "" {
			fmt.Fprintf(os.Stderr,
				"%q matched more than one org, but %q (%s) "+
					"seemed the most likely\n",
				org, fm.Name, fm.UUID)
		}
		a, err := db.ApplianceIDsByOrgID(ctx, fm.UUID)
		if err != nil {
			return err
		}
		apps = append(apps, a...)
	}

	if len(apps) == 0 {
		apps, err = db.AllApplianceIDs(ctx)
		if err != nil {
			return err
		}
	}

	// XXX We could write a query with a WHERE clause ...
	// XXX It might also be nice to have pattern matching.
	// XXX And sorting
	matchingApps := make([]appliancedb.ApplianceID, 0)
	for _, app := range apps {
		if (reg.Project == "" || reg.Project == app.GCPProject) &&
			(reg.Region == "" || reg.Region == app.GCPRegion) &&
			(reg.Registry == "" || reg.Registry == app.ApplianceReg) &&
			(appID == "" || appID == app.ApplianceRegID) {
			matchingApps = append(matchingApps, app)
		}
	}

	if len(matchingApps) == 0 {
		return nil
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "UUID"},
		prettytable.Column{Header: "Site"},
		prettytable.Column{Header: "Project"},
		prettytable.Column{Header: "Region"},
		prettytable.Column{Header: "Registry"},
		prettytable.Column{Header: "Appliance Name"},
	)
	table.Separator = "  "

	for _, app := range matchingApps {
		table.AddRow(app.ApplianceUUID, app.SiteUUID,
			app.GCPProject, app.GCPRegion,
			app.ApplianceReg, app.ApplianceRegID)
	}
	table.Print()
	return nil
}

func newAppliance(cmd *cobra.Command, args []string) error {
	var err error
	appID := args[0]
	siteUUID := args[1]
	outdir, _ := cmd.Flags().GetString("directory")
	appUUID, _ := cmd.Flags().GetString("uuid")
	hwSerial, _ := cmd.Flags().GetString("hw-serial")
	mac, _ := cmd.Flags().GetString("mac-address")
	noEscrow, _ := cmd.Flags().GetBool("no-escrow")

	var appUU uuid.UUID
	if appUUID != "" {
		var err error
		if appUU, err = uuid.FromString(appUUID); err != nil {
			return err
		}
	} else {
		appUU = uuid.NewV4()
	}

	var siteUU uuid.UUID
	if siteUUID == "null" {
		siteUU = appliancedb.NullSiteUUID
	} else {
		siteUU, err = uuid.FromString(siteUUID)
		if err != nil {
			return err
		}
	}

	db, reg, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	// Creating the appliance object will put it in the database and save
	// the cloud secret in Vault (unless noEscrow is true).  If noEscrow is
	// true, we'll attempt to write the secret to a file.  If either that or
	// the escrow fails, we'll emit the secret (if we got it) to stdout.
	var jout []byte
	var vaultPath string
	appUU, _, _, jout, vaultPath, err = registry.NewAppliance(context.Background(),
		db, appUU, siteUU, reg.Project, reg.Region, reg.Registry, appID,
		hwSerial, mac, os.Getenv("B10E_CLREG_VAULT_PUBKEY_PATH"),
		os.Getenv("B10E_CLREG_VAULT_PUBKEY_COMPONENT"), noEscrow)
	if err != nil {
		// Only exit if we didn't get the secret bytes back; otherwise,
		// the escrow failed, and that's recoverable.
		if jout == nil {
			return errors.Wrap(err, "failed to create new appliance")
		}
	}

	var ioerr error
	var secretsFile string
	if noEscrow || outdir != "" {
		if outdir == "" {
			outdir, ioerr = os.Getwd()
		}
		if ioerr == nil {
			if ioerr = os.MkdirAll(outdir, 0700); ioerr == nil {
				secretsFile = outdir + "/" + appID + ".cloud.secret.json"
				ioerr = ioutil.WriteFile(secretsFile, jout, 0600)
			}
		}
	}

	fmt.Printf("-------------------------------------------------------------\n")
	fmt.Printf("Created device: projects/%s/locations/%s/registries/%s/appliances/%s\n",
		reg.Project, reg.Region, reg.Registry, appID)
	fmt.Printf("     Site UUID: %s\n", siteUU)
	fmt.Printf("Appliance UUID: %s\n", appUU)
	fmt.Printf("-------------------------------------------------------------\n")

	// Emit errors, if any.
	if ioerr != nil {
		fmt.Printf("Secrets file couldn't be written: %v\n", ioerr)
	}
	if err != nil {
		fmt.Printf("Secret couldn't be escrowed: %v\n", err)
		fmt.Printf("You can try again later by running the command:\n")
		fmt.Printf("    cat <data> | vault kv put %s cloud_secret=-\n", vaultPath)
	}
	if err != nil || ioerr != nil {
		fmt.Printf("-------------------------------------------------------------\n")
	}

	// Emit paths, if requested and successful.
	if secretsFile != "" && ioerr == nil {
		fmt.Printf("  Secrets file: %s\n", secretsFile)
	}
	if !noEscrow && err == nil {
		fmt.Printf("    Vault path: %s\n", vaultPath)
		fmt.Printf("The secret can be retrieved with the command:\n")
		fmt.Printf("    vault kv get -field cloud_secret %s\n", vaultPath)
	}

	// If neither file nor escrow succeeded, or if one failed and the other
	// wasn't requested, emit the secret to stdout.
	if (secretsFile == "" && err != nil) || (noEscrow && ioerr != nil) || (err != nil && ioerr != nil) {
		fmt.Printf("%s\n", jout)
	}

	// Tell the user what to do with the secret.
	fmt.Printf("-------------------------------------------------------------\n")
	fmt.Printf("Next, provision the secret to the appliance at:\n")
	fmt.Printf("    /data/secret/rpcd/cloud.secret.json\n")
	fmt.Printf("    /var/spool/secret/rpcd/cloud.secret.json (on Debian)\n")

	// The app will print the returned error at the end; we don't need it to
	// duplicate what we already printed above, but it should exit 1 and
	// print some error if we failed to write the key somewhere.
	if err != nil {
		return errors.New("failed to escrow cloud secret")
	} else if ioerr != nil {
		return errors.New("failed to write cloud secret to disk")
	}
	return nil
}

func setApp(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	siteUUID, _ := cmd.Flags().GetString("site-uuid")

	var siteUU *uuid.UUID
	if siteUUID != "" {
		var u uuid.UUID
		var err error
		if u, err = uuid.FromString(siteUUID); err != nil {
			return err
		}
		siteUU = &u
	}

	uu := args[0]
	appUUID := uuid.Must(uuid.FromString(uu))
	app, err := db.ApplianceIDByUUID(ctx, appUUID)
	if err != nil {
		return err
	}

	if siteUU != nil {
		app.SiteUUID = *siteUU
	}

	err = db.UpdateApplianceID(ctx, app)
	if err == nil {
		fmt.Printf("Updated appliance %+v\n", app)
	}
	return err
}

func appMain(rootCmd *cobra.Command) {
	appCmd := &cobra.Command{
		Use:   "app <subcmd> [flags] [args]",
		Short: "Administer appliances in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(appCmd)

	newAppCmd := &cobra.Command{
		Use:   "new [flags] <appliance name> <siteUUID>|null",
		Args:  cobra.ExactArgs(2),
		Short: "Create an appliance and add it to the registry; use 'null' for the site UUID to specify no associated site",
		RunE:  newAppliance,
	}
	newAppCmd.Flags().StringP("directory", "d", "", "output directory")
	newAppCmd.Flags().StringP("project", "p", "", "GCP project")
	newAppCmd.Flags().StringP("region", "R", "", "GCP region")
	newAppCmd.Flags().StringP("registry", "r", "", "appliance registry")
	newAppCmd.Flags().StringP("hw-serial", "", "", "representative system HW serial")
	newAppCmd.Flags().StringP("mac-address", "", "", "representative system MAC address")
	newAppCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	newAppCmd.Flags().StringP("uuid", "u", "", "appliance UUID")
	newAppCmd.Flags().StringP("site-uuid", "s", "", "site UUID")
	newAppCmd.Flags().BoolP("no-escrow", "", false, "don't escrow the private key in Vault")
	appCmd.AddCommand(newAppCmd)

	listAppCmd := &cobra.Command{
		Use:     "list [flags]",
		Args:    cobra.NoArgs,
		Short:   "List appliances in the registry",
		Aliases: []string{"ls"},
		RunE:    listAppliances,
	}
	listAppCmd.Flags().StringP("project", "p", "", "GCP project")
	listAppCmd.Flags().StringP("region", "R", "", "GCP region")
	listAppCmd.Flags().StringP("registry", "r", "", "appliance registry")
	listAppCmd.Flags().StringP("name", "n", "", "appliance name")
	listAppCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	listAppCmd.Flags().StringSliceP("org", "o", []string{}, "list appliances belonging to these orgs")
	listAppCmd.Flags().StringSliceP("site", "s", []string{}, "list appliances at these sites")
	appCmd.AddCommand(listAppCmd)

	setAppCmd := &cobra.Command{
		Use:   "set [flags] <uuid>",
		Args:  cobra.ExactArgs(1),
		Short: "Set appliance properties",
		RunE:  setApp,
	}
	setAppCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	setAppCmd.Flags().StringP("site-uuid", "s", "", "site UUID")
	appCmd.AddCommand(setAppCmd)
}

