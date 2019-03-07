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
	"bg/cloud_models/appliancedb"
	"context"
	"fmt"
	"strings"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
)

func printPrefixedTable(table *prettytable.Table, prefix string) {
	tabStr := table.String()
	tabRows := strings.Split(tabStr, "\n")
	for _, row := range tabRows {
		fmt.Printf("%s%s\n", prefix, row)
	}
}

func listAccounts(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	orgs, err := db.AllOrganizations(ctx)
	if err != nil {
		return err
	}
	for _, org := range orgs {
		accts, err := db.AccountsByOrganization(ctx, org.UUID)
		if err != nil {
			return err
		}
		if org.UUID == uuid.Nil {
			continue
		}
		if len(accts) == 0 {
			fmt.Printf("Organization: %s (%s):\n  No accounts\n", org.Name, org.UUID)
			continue
		}
		fmt.Printf("Organization: %q (%s)\n", org.Name, org.UUID)
		table, _ := prettytable.NewTable(
			prettytable.Column{Header: "UUID"},
			prettytable.Column{Header: "Email"},
			prettytable.Column{Header: "Phone"},
		)
		for _, acct := range accts {
			table.AddRow(acct.UUID, acct.Email, acct.PhoneNumber)
		}
		printPrefixedTable(table, "  ")
	}
	return nil
}

func infoAccount(cmd *cobra.Command, args []string) error {
	acctUUID := uuid.Must(uuid.FromString(args[0]))
	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	ai, err := registry.GetAccountInformation(ctx, db, acctUUID)
	if err != nil {
		return err
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "KEY"},
		prettytable.Column{Header: "VALUE"},
	)
	table.Separator = "  "
	table.AddRow("UUID", ai.Account.UUID)
	table.AddRow("Email", ai.Account.Email)
	table.AddRow("Phone", ai.Account.PhoneNumber)
	table.AddRow("Organization.UUID", ai.Organization.UUID)
	table.AddRow("Organization.Name", ai.Organization.Name)
	table.AddRow("Person.UUID", ai.Person.UUID)
	table.AddRow("Person.Name", ai.Person.Name)
	table.AddRow("Person.PrimaryEmail", ai.Person.PrimaryEmail)
	for i, id := range ai.OAuth2IDs {
		prefix := fmt.Sprintf("OAuth2ID.%d.", i)
		table.AddRow(prefix+"ID", id.ID)
		table.AddRow(prefix+"Provider", id.Provider)
		table.AddRow(prefix+"Subject", id.Subject)
	}
	table.Print()
	return nil
}

func listAccountRoles(cmd *cobra.Command, args []string) error {
	acctUUID := uuid.Must(uuid.FromString(args[0]))
	ctx := context.Background()

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	roles, err := db.AccountOrgRolesByAccount(ctx, acctUUID)

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "Organization"},
		prettytable.Column{Header: "Role"},
	)
	table.Separator = "  "
	for _, role := range roles {
		table.AddRow(role.OrganizationUUID, role.Role)
	}
	table.Print()

	return nil
}

func modAccountRole(cmd *cobra.Command, args []string) error {
	var err error
	acctUUID := uuid.Must(uuid.FromString(args[0]))
	ctx := context.Background()
	role := args[1]
	orgUUIDstr, _ := cmd.Flags().GetString("organization")

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	ai, err := registry.GetAccountInformation(ctx, db, acctUUID)
	if err != nil {
		return err
	}
	var orgUUID uuid.UUID
	if orgUUIDstr != "" {
		orgUUID = uuid.Must(uuid.FromString(orgUUIDstr))
	} else {
		orgUUID = ai.Organization.UUID
	}
	r := appliancedb.AccountOrgRole{
		AccountUUID:      acctUUID,
		OrganizationUUID: orgUUID,
		Role:             role,
	}
	p := ""
	if cmd.Name() == "add" {
		err = db.InsertAccountOrgRole(ctx, &r)
		p = "Added"
	} else if cmd.Name() == "delete" {
		err = db.DeleteAccountOrgRole(ctx, &r)
		p = "Deleted"
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s <%s %s %s>\n", p, acctUUID.String(), orgUUID.String(), role)
	return nil
}

func accountMain(rootCmd *cobra.Command) {
	accountCmd := &cobra.Command{
		Use:   "account <subcmd> [flags] [args]",
		Short: "Administer accounts in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(accountCmd)

	listAccountCmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List accounts in the registry",
		RunE:  listAccounts,
	}
	listAccountCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	accountCmd.AddCommand(listAccountCmd)

	infoAccountCmd := &cobra.Command{
		Use:   "info",
		Args:  cobra.ExactArgs(1),
		Short: "Get extended information about an account in the registry",
		RunE:  infoAccount,
	}
	infoAccountCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	accountCmd.AddCommand(infoAccountCmd)

	roleAccountCmd := &cobra.Command{
		Use:   "role <subcmd> [flags] [args]",
		Args:  cobra.NoArgs,
		Short: "Get information about account roles",
	}
	accountCmd.AddCommand(roleAccountCmd)

	listRoleAccountCmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.ExactArgs(1),
		Short: "List account roles",
		RunE:  listAccountRoles,
	}
	roleAccountCmd.AddCommand(listRoleAccountCmd)

	addRoleAccountCmd := &cobra.Command{
		Use:   "add [-o organization-uuid] account-uuid role",
		Args:  cobra.ExactArgs(2),
		Short: "Add {account,org} role",
		RunE:  modAccountRole,
	}
	addRoleAccountCmd.Flags().StringP("organization", "o", "", "organization UUID for this role")
	roleAccountCmd.AddCommand(addRoleAccountCmd)

	deleteRoleAccountCmd := &cobra.Command{
		Use:   "delete [-o organization-uuid] account-uuid role",
		Args:  cobra.ExactArgs(2),
		Short: "Delete {account,org} role",
		RunE:  modAccountRole,
	}
	deleteRoleAccountCmd.Flags().StringP("organization", "o", "", "organization UUID for this role")
	roleAccountCmd.AddCommand(deleteRoleAccountCmd)

}