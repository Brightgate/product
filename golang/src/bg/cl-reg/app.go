/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
)

func listAppliances(cmd *cobra.Command, args []string) error {
	appID, _ := cmd.Flags().GetString("name")
	siteUUID, _ := cmd.Flags().GetString("site-uuid")

	db, reg, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	apps, err := db.AllApplianceIDs(context.Background())
	if err != nil {
		return err
	}

	u, err := registry.SiteUUIDByNameFuzzy(context.Background(), db, siteUUID)
	if err != nil {
		if ase, ok := err.(registry.AmbiguousSiteError); ok {
			fmt.Fprint(os.Stderr, ase.Pretty())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if u.SiteName != "" {
		fmt.Fprintf(os.Stderr,
			"%q matched more than one site, but %q (%s) seemed the most likely\n",
			siteUUID, u.SiteName, u.SiteUUID)
	}
	if u.SiteUUID != uuid.Nil {
		siteUUID = u.SiteUUID.String()
	}

	// XXX We could write a query with a WHERE clause ...
	// XXX It might also be nice to have pattern matching.
	// XXX And sorting
	matchingApps := make([]appliancedb.ApplianceID, 0)
	for _, app := range apps {
		if (reg.Project == "" || reg.Project == app.GCPProject) &&
			(reg.Region == "" || reg.Region == app.GCPRegion) &&
			(reg.Registry == "" || reg.Registry == app.ApplianceReg) &&
			(siteUUID == "" || app.SiteUUID.String() == siteUUID) &&
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
		hwSerial, mac, noEscrow)
	if err != nil {
		// Only exit if we didn't get the secret bytes back; otherwise,
		// the escrow failed, and that's recoverable.
		if jout == nil {
			return err
		}
	}

	var ioerr error
	var secretsFile string
	if noEscrow {
		if ioerr = os.MkdirAll(outdir, 0700); ioerr == nil {
			secretsFile = outdir + "/" + appID + ".cloud.secret.json"
			ioerr = ioutil.WriteFile(secretsFile, jout, 0600)
		}
	}

	fmt.Printf("-------------------------------------------------------------\n")
	fmt.Printf("Created device: projects/%s/locations/%s/registries/%s/appliances/%s\n",
		reg.Project, reg.Region, reg.Registry, appID)
	fmt.Printf("     Site UUID: %s\n", siteUU)
	fmt.Printf("Appliance UUID: %s\n", appUU)
	if secretsFile != "" && ioerr == nil {
		fmt.Printf("  Secrets file: %s\n", secretsFile)
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("Next, provision %s to the appliance at:\n", secretsFile)
		fmt.Printf("    /data/secret/rpcd/cloud.secret.json\n")
		fmt.Printf("    /var/spool/secret/rpcd/cloud.secret.json (on Debian)\n")
	} else if err != nil || ioerr != nil {
		// If err != nil at this point, that means we tried to escrow,
		// but failed.  If ioerr != nil, we tried to write the file, but
		// didn't try to escrow.  So only one would be non-nil.
		if ioerr == nil {
			ioerr = err
			// The app will print the error at the end; we don't
			// need it duplicated, but it should exit 1 and print
			// some error.
			err = errors.New("failed to escrow key")
		}
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("Secrets file couldn't be written/escrowed: %v\n", ioerr)
		fmt.Printf("Copy the following to the appliance at:\n")
		fmt.Printf("    /data/secret/rpcd/cloud.secret.json\n")
		fmt.Printf("    /var/spool/secret/rpcd/cloud.secret.json (on Debian)\n")
		fmt.Printf("and try to write to Vault with the command:\n")
		fmt.Printf("    cat <data> | vault kv put %s cloud_secret=-\n", vaultPath)
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("%s\n", jout)
		fmt.Printf("-------------------------------------------------------------\n")
	} else {
		fmt.Printf("    Vault path: %s\n", vaultPath)
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("Next, provision secret to the appliance at:\n")
		fmt.Printf("    /data/secret/rpcd/cloud.secret.json\n")
		fmt.Printf("    /var/spool/secret/rpcd/cloud.secret.json (on Debian)\n")
		fmt.Printf("The secret can be retrieved with the command:\n")
		fmt.Printf("    vault kv get -field cloud_secret %s\n", vaultPath)
	}

	return err
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
	newAppCmd.Flags().StringP("directory", "d", ".", "output directory")
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
	listAppCmd.Flags().StringP("site-uuid", "s", "", "site UUID")
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
