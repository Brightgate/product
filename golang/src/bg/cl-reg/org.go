/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package main

import (
	"bg/cl_common/registry"
	"context"
	"fmt"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
)

func newOrg(cmd *cobra.Command, args []string) error {
	orgName := args[0]

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	orgUU, err := registry.NewOrganization(context.Background(), db, orgName)
	if err != nil {
		return err
	}
	fmt.Printf("Created Org: uuid=%s, name='%s'\n", orgUU, orgName)
	return nil
}

func listOrgs(cmd *cobra.Command, args []string) error {
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	orgs, err := db.AllOrganizations(context.Background())
	if err != nil {
		return err
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "UUID"},
		prettytable.Column{Header: "Name"},
	)
	table.Separator = "  "

	for _, org := range orgs {
		table.AddRow(org.UUID, org.Name)
	}
	table.Print()
	return nil
}

func setOrg(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	name, _ := cmd.Flags().GetString("name")

	uu := args[0]
	orgUUID := uuid.Must(uuid.FromString(uu))
	org, err := db.OrganizationByUUID(ctx, orgUUID)
	if err != nil {
		return err
	}

	if name != "" {
		org.Name = name
	}
	err = db.UpdateOrganization(ctx, org)
	if err == nil {
		fmt.Printf("Updated organization %+v\n", org)
	}
	return err
}

func orgMain(rootCmd *cobra.Command) {
	orgCmd := &cobra.Command{
		Use:   "org <subcmd> [flags] [args]",
		Short: "Administer organizations in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(orgCmd)

	newOrgCmd := &cobra.Command{
		Use:   "new [flags] <org name>",
		Args:  cobra.ExactArgs(1),
		Short: "Create an org and add it to the registry",
		RunE:  newOrg,
	}
	newOrgCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	orgCmd.AddCommand(newOrgCmd)

	listOrgCmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List organizations in the registry",
		RunE:  listOrgs,
	}
	listOrgCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	orgCmd.AddCommand(listOrgCmd)

	setOrgCmd := &cobra.Command{
		Use:   "set [flags] <uuid>",
		Args:  cobra.ExactArgs(1),
		Short: "Set organization properties",
		RunE:  setOrg,
	}
	setOrgCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	setOrgCmd.Flags().StringP("name", "n", "", "set organization name")
	orgCmd.AddCommand(setOrgCmd)
}
