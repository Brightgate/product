/*
 * Copyright 2020 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"
	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
	"golang.org/x/oauth2/google"
)

func newSite(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	siteName := args[0]
	orgUUID := uuid.Must(uuid.FromString(args[1]))

	creds, _ := google.FindDefaultCredentials(ctx, storage.ScopeFullControl)
	if creds == nil {
		return fmt.Errorf("no cloud credentials defined")
	}

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	as, err := hex.DecodeString(environ.AccountSecret)
	if err != nil {
		return err
	}
	if len(as) == 0 {
		fmt.Printf("Warning: B10E_CLREG_ACCOUNT_SECRET not set in the environment; accounts won't sync")
	}
	db.AccountSecretsSetPassphrase(as)

	siteUU, siteCS, err := registry.NewSite(ctx, db, creds.ProjectID, siteName, orgUUID)
	if err != nil {
		return err
	}
	fmt.Printf("Created Site: uuid=%s, name='%s' organization='%s'\n", siteUU, siteName, orgUUID)
	fmt.Printf("Created Bucket: provider=%s, name='%s'\n", siteCS.Provider, siteCS.Bucket)

	if orgUUID == appliancedb.NullOrganizationUUID {
		fmt.Printf("Warning: null organization; usually for testing only\n")
		return nil
	}

	site, err := db.CustomerSiteByUUID(ctx, siteUU)
	if err != nil {
		return err
	}

	accounts, err := db.AccountsByOrganization(ctx, orgUUID)
	if err != nil {
		return err
	}

	fmt.Printf("Syncing accounts:\n")
	for _, acct := range accounts {
		err = registry.SyncAccountSelfProv(ctx, db, getConfig, acct.UUID,
			[]appliancedb.CustomerSite{*site}, true)
		if err != nil {
			fmt.Printf("  Sync Error <%s>: %v\n", acct.Email, err)
		} else {
			fmt.Printf("  Sync    OK <%s>\n", acct.Email)
		}
	}
	return nil
}

func listSites(cmd *cobra.Command, args []string) error {
	orgsArg, _ := cmd.Flags().GetStringSlice("org")
	sitesArg, _ := cmd.Flags().GetStringSlice("site")

	if len(orgsArg) > 0 && len(sitesArg) > 0 {
		return fmt.Errorf("Only one of --org and --site may be specified")
	}

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	var sites []appliancedb.CustomerSite
	for _, site := range sitesArg {
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
		s, err := db.CustomerSiteByUUID(ctx, fm.UUID)
		if err != nil {
			return err
		}
		sites = append(sites, *s)
	}

	for _, org := range orgsArg {
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
		s, err := db.CustomerSitesByOrganization(ctx, fm.UUID)
		if err != nil {
			return err
		}
		sites = append(sites, s...)
	}

	if len(sites) == 0 {
		sites, err = db.AllCustomerSites(ctx)
		if err != nil {
			return err
		}
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "UUID"},
		prettytable.Column{Header: "OrganizationUUID"},
		prettytable.Column{Header: "Name"},
	)
	table.Separator = "  "

	for _, site := range sites {
		table.AddRow(site.UUID, site.OrganizationUUID, site.Name)
	}
	table.Print()
	return nil
}

func setSite(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	name, _ := cmd.Flags().GetString("name")
	orgUU, _ := cmd.Flags().GetString("org-uuid")

	uu := args[0]
	siteUUID := uuid.Must(uuid.FromString(uu))
	site, err := db.CustomerSiteByUUID(ctx, siteUUID)
	if err != nil {
		return err
	}

	if name != "" {
		site.Name = name
	}
	if orgUU != "" {
		site.OrganizationUUID = uuid.Must(uuid.FromString(orgUU))
	}

	err = db.UpdateCustomerSite(ctx, site)
	if err == nil {
		fmt.Printf("Updated site %+v\n", site)
	}
	return err
}

func siteMain(rootCmd *cobra.Command) {
	siteCmd := &cobra.Command{
		Use:   "site <subcmd> [flags] [args]",
		Short: "Administer sites in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(siteCmd)

	newSiteCmd := &cobra.Command{
		Use:   "new [flags] <site name> <organization-uuid>",
		Args:  cobra.ExactArgs(2),
		Short: "Create a site and add it to the registry",
		RunE:  newSite,
	}
	newSiteCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	siteCmd.AddCommand(newSiteCmd)

	listSiteCmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List sites in the registry",
		RunE:  listSites,
	}
	listSiteCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	listSiteCmd.Flags().StringSliceP("org", "o", []string{}, "list sites belonging to these orgs")
	listSiteCmd.Flags().StringSliceP("site", "s", []string{}, "list these sites")
	siteCmd.AddCommand(listSiteCmd)

	setSiteCmd := &cobra.Command{
		Use:   "set [flags] <uuid>",
		Args:  cobra.ExactArgs(1),
		Short: "Set site properties; valid: 'name'",
		RunE:  setSite,
	}
	setSiteCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	setSiteCmd.Flags().StringP("name", "n", "", "set site name")
	setSiteCmd.Flags().StringP("org-uuid", "", "", "set site's organization uuid")
	siteCmd.AddCommand(setSiteCmd)
}

