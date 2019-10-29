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

func newOAuth2OrgRule(cmd *cobra.Command, args []string) error {
	provider := args[0]
	ruleType := appliancedb.OAuth2OrgRuleType(args[1])
	ruleValue := args[2]
	organization := args[3]
	orgUU := uuid.Must(uuid.FromString(organization))

	if ruleType != appliancedb.RuleTypeTenant &&
		ruleType != appliancedb.RuleTypeDomain &&
		ruleType != appliancedb.RuleTypeEmail {
		return fmt.Errorf("Invalid rule type %q; use 'tenant', 'domain', or 'email'", ruleType)
	}

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	err = registry.NewOAuth2OrganizationRule(context.Background(), db, provider,
		ruleType, ruleValue, orgUU)
	if err != nil {
		return err
	}
	fmt.Printf("Created OAuth2OrgRule: provider=%q, ruleType=%q ruleValue=%q, org=%q\n",
		provider, ruleType, ruleValue, orgUU)
	return nil
}

func delOAuth2OrgRule(cmd *cobra.Command, args []string) error {
	provider := args[0]
	ruleType := appliancedb.OAuth2OrgRuleType(args[1])
	ruleValue := args[2]

	if ruleType != appliancedb.RuleTypeTenant &&
		ruleType != appliancedb.RuleTypeDomain &&
		ruleType != appliancedb.RuleTypeEmail {
		return fmt.Errorf("Invalid rule type %q; use 'tenant', 'domain', or 'email'", ruleType)
	}

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	rule, err := registry.DeleteOAuth2OrganizationRule(context.Background(), db, provider,
		ruleType, ruleValue)
	if err != nil {
		return err
	}
	fmt.Printf("Deleted OAuth2OrgRule: provider=%q, ruleType=%q ruleValue=%q org=%q\n",
		rule.Provider, rule.RuleType, rule.RuleValue, rule.OrganizationUUID)
	return nil
}

func listOAuth2OrgRules(cmd *cobra.Command, args []string) error {
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	rules, err := db.AllOAuth2OrganizationRules(context.Background())
	if err != nil {
		return err
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "Provider"},
		prettytable.Column{Header: "RuleType"},
		prettytable.Column{Header: "RuleValue"},
		prettytable.Column{Header: "OrganizationUUID"},
	)
	table.Separator = "  "

	for _, rule := range rules {
		_ = table.AddRow(rule.Provider, string(rule.RuleType),
			rule.RuleValue, rule.OrganizationUUID.String())
	}
	table.Print()
	return nil
}

func oauth2Main(rootCmd *cobra.Command) {
	oauth2OrgRuleCmd := &cobra.Command{
		Use:   "oauth2_org_rule <subcmd> [flags] [args]",
		Short: "Administer OAuth2OrgRules in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(oauth2OrgRuleCmd)

	newOAuth2OrgRuleCmd := &cobra.Command{
		Use:   "new [flags] <provider> [tenant|domain|email] <value> <organization-uuid>",
		Args:  cobra.ExactArgs(4),
		Short: "Create an OAuth2OrgRule and add it to the registry",
		RunE:  newOAuth2OrgRule,
	}
	newOAuth2OrgRuleCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	oauth2OrgRuleCmd.AddCommand(newOAuth2OrgRuleCmd)

	listOAuth2OrgRuleCmd := &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List OAuth2OrgRules in the registry",
		RunE:  listOAuth2OrgRules,
	}
	listOAuth2OrgRuleCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	oauth2OrgRuleCmd.AddCommand(listOAuth2OrgRuleCmd)

	delOAuth2OrgRuleCmd := &cobra.Command{
		Use:   "del [flags] <provider> [tenant|domain|email] <value>",
		Args:  cobra.ExactArgs(3),
		Short: "Delete OAuth2OrgRule in the registry",
		RunE:  delOAuth2OrgRule,
	}
	delOAuth2OrgRuleCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	oauth2OrgRuleCmd.AddCommand(delOAuth2OrgRuleCmd)
}
