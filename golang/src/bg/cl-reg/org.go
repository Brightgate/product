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
	"context"
	"fmt"

	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"

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

func newOrgRel(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	orgUU, err := uuid.FromString(args[0])
	if err != nil {
		return err
	}
	tgtUU, err := uuid.FromString(args[1])
	if err != nil {
		return err
	}
	relType := args[2]

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	uu := uuid.NewV4()
	rel := &appliancedb.OrgOrgRelationship{
		UUID:                   uu,
		OrganizationUUID:       orgUU,
		TargetOrganizationUUID: tgtUU,
		Relationship:           relType,
	}
	err = db.InsertOrgOrgRelationship(ctx, rel)
	fmt.Printf("Created Org/Org Relationship uuid=%s: %s ---%s--> %s\n", uu, orgUU, relType, tgtUU)
	return nil
}

func listOrgRel(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	verbose, _ := cmd.Flags().GetBool("verbose")

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	orgs, err := db.AllOrganizations(ctx)
	if err != nil {
		return err
	}
	var table *prettytable.Table
	if verbose {
		table, _ = prettytable.NewTable(
			prettytable.Column{Header: "UUID"},
			prettytable.Column{Header: "Organization"},
			prettytable.Column{Header: "OrgName"},
			prettytable.Column{Header: "TargetOrganization"},
			prettytable.Column{Header: "TargetName"},
			prettytable.Column{Header: "Relationship"},
		)
	} else {
		table, _ = prettytable.NewTable(
			prettytable.Column{Header: "UUID"},
			prettytable.Column{Header: "Organization"},
			prettytable.Column{Header: "TargetOrganization"},
			prettytable.Column{Header: "Relationship"},
		)
	}
	table.Separator = "  "
	for _, org := range orgs {
		rels, err := db.OrgOrgRelationshipsByOrg(ctx, org.UUID)
		if err != nil {
			return err
		}
		for _, rel := range rels {
			tgtOrg, err := db.OrganizationByUUID(ctx,
				rel.TargetOrganizationUUID)
			if err != nil {
				return err
			}
			if verbose {
				table.AddRow(rel.UUID, org.UUID, org.Name,
					tgtOrg.UUID, tgtOrg.Name, rel.Relationship)
			} else {
				table.AddRow(rel.UUID, org.UUID,
					tgtOrg.UUID, rel.Relationship)
			}
		}
	}
	table.Print()

	return nil
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

	orgRelCmd := &cobra.Command{
		Use:   "relationship <subcmd> [flags] [args]",
		Short: "List, add and remove org/org relationships",
		Args:  cobra.NoArgs,
	}
	orgCmd.AddCommand(orgRelCmd)

	newOrgRelCmd := &cobra.Command{
		Use:   "new [flags] <org uuid> <target org uuid> self|msp",
		Args:  cobra.ExactArgs(3),
		Short: "Create an org and add it to the registry",
		RunE:  newOrgRel,
	}
	newOrgRelCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	orgRelCmd.AddCommand(newOrgRelCmd)

	listOrgRelCmd := &cobra.Command{
		Use:   "list [flags]",
		Args:  cobra.NoArgs,
		Short: "List Org/Org relationships",
		RunE:  listOrgRel,
	}
	listOrgRelCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	listOrgRelCmd.Flags().BoolP("verbose", "v", false, "verbose output")
	orgRelCmd.AddCommand(listOrgRelCmd)
}
