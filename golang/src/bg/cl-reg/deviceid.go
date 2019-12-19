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
	"log"
	"strings"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgapi"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/satori/uuid"
	"github.com/spf13/cobra"
)

var model2prop = map[string]string{
	"bayes-os-4":     "classification/os_genus",
	"bayes-device-3": "classification/device_genus",
	"lookup-mfg":     "classification/oui_mfg",
}

type classification struct {
	Mac            string
	Model          string
	PropName       string
	Classification string
}

// sqliteClassifications emits classifications from a sqlite db to the given channel
func sqliteClassifications(ctx context.Context, siteUUID uuid.UUID, db *sqlx.DB, output chan classification) error {
	rows, err := db.Query("SELECT mac, model_name, classification FROM classification WHERE site_uuid=?", siteUUID.String())
	if err != nil {
		log.Printf("failed syncing %s: %s\n", siteUUID.String(), err)
		return err
	}

	for rows.Next() {
		var c classification
		err = rows.Scan(&c.Mac, &c.Model, &c.Classification)
		if err != nil {
			return err
		}
		c.PropName = model2prop[c.Model]
		if c.PropName == "" {
			continue
		}
		log.Printf("built classification %v", c)
		output <- c
	}
	return nil
}

func syncSiteClassifications(ctx context.Context, siteUUID uuid.UUID, classifications chan classification) {
	log.Printf("started syncing %s\n", siteUUID.String())

	// macPropOps[<macaddr>][<propname>] -> propOp (del or add)
	macPropOps := make(map[string]map[string]cfgapi.PropertyOp)

	cfg, err := getConfig(siteUUID.String())
	if err != nil {
		log.Printf("failed syncing %s: config handle: %s\n", siteUUID.String(), err)
		return
	}

	clients, err := cfg.GetProps("@/clients")
	if err != nil {
		log.Printf("failed syncing %s: Get @/clients : %s\n", siteUUID.String(), err)
		return
	}

	// For every client classification property we see in the tree,
	// generate propop groups which clear out the classification info,
	// grouped by client.
	log.Printf("generating clearouts for classification props")
	for cmac := range clients.Children {
		deletes := make([]string, 0)
		for _, prop := range model2prop {
			if macPropOps[cmac] == nil {
				macPropOps[cmac] = make(map[string]cfgapi.PropertyOp)
			}
			propPath := fmt.Sprintf("@/clients/%s/%s", cmac, prop)
			oldValue, err := cfg.GetProp(propPath)
			if err == nil && oldValue != "" {
				macPropOps[cmac][prop] = cfgapi.PropertyOp{
					Op:   cfgapi.PropDelete,
					Name: propPath,
				}
				deletes = append(deletes, prop)
			}
		}
		if len(deletes) > 0 {
			log.Printf("\t%s: %s", cmac, strings.Join(deletes, ", "))
		}
	}

	log.Printf("ingesting classification records")
	// Consume classification data in the channel ...
	for c := range classifications {
		// ... skip if the client is not present in the tree
		clientPath := fmt.Sprintf("@/clients/%s", c.Mac)
		_, err := cfg.GetProp(clientPath)
		if err != nil {
			log.Printf("\tskipping client %s; not in tree", clientPath)
			continue
		}
		propPath := fmt.Sprintf("@/clients/%s/%s", c.Mac, c.PropName)
		oldValue, _ := cfg.GetProp(propPath)
		// ... skip if the old and new values are the same, and shoot
		// down the removal of the property.
		if oldValue == c.Classification {
			delete(macPropOps[c.Mac], c.PropName)
			log.Printf("\tskip %s=%q; already set", c.PropName, c.Classification)
			continue
		}
		// ... else create the property
		log.Printf("\t%s=%q\n", propPath, c.Classification)
		if macPropOps[c.Mac] == nil {
			macPropOps[c.Mac] = make(map[string]cfgapi.PropertyOp)
		}
		macPropOps[c.Mac][c.PropName] = cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  propPath,
			Value: c.Classification,
		}
	}

	log.Printf("finished ingesting classification records")
	// Work through the per-client PropOps.  Generate a PropOp array for
	// each, along with a guarding PropTest (so we don't accidentally
	// recreate a deleted client).  Then execute.
	for cmac, cPropOpMap := range macPropOps {
		if len(cPropOpMap) == 0 {
			continue
		}
		ops := []cfgapi.PropertyOp{
			{
				Op:   cfgapi.PropTest,
				Name: fmt.Sprintf("@/clients/%s", cmac),
			},
		}
		for _, pOp := range cPropOpMap {
			ops = append(ops, pOp)
		}
		log.Printf("syncing %s:@/client/%s\n\t%v\n", siteUUID.String(), cmac, ops)
		_, err := cfg.Execute(ctx, ops).Wait(ctx)
		if err != nil {
			log.Printf("error on cfg execute/wait: %s", err)
		}
	}

	log.Printf("done syncing %s\n", siteUUID.String())
}

func syncDeviceID(cmd *cobra.Command, args []string) error {
	if environ.ConfigdConnection == "" {
		return fmt.Errorf("Must set B10E_CLREG_CLCONFIGD_CONNECTION")
	}

	orgStr, _ := cmd.Flags().GetString("org")
	siteStr, _ := cmd.Flags().GetString("site")
	allOrgs, _ := cmd.Flags().GetBool("all")
	if allOrgs && (orgStr != "" || siteStr != "") {
		return fmt.Errorf("Can't specify --all and either --site or --org")
	}
	if !allOrgs && orgStr == "" && siteStr == "" {
		return fmt.Errorf("Must specify at least one of --all, --site, or --org")
	}

	ctx := context.Background()
	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}
	defer db.Close()

	var sqliteDB *sqlx.DB
	if cmd.Use == "sync" {
		obsFile, _ := cmd.Flags().GetString("sqlite-src")
		if obsFile == "" {
			return fmt.Errorf("must specify a --sqlite-src source database")
		}
		obsDSN := fmt.Sprintf("file:%s?mode=ro", obsFile)
		sqliteDB, err = sqlx.Connect("sqlite3", obsDSN)
		if err != nil {
			log.Fatalf("database open: %v\n", err)
		}
		defer sqliteDB.Close()
	}

	var orgs []appliancedb.Organization
	sites := make(map[uuid.UUID]appliancedb.CustomerSite)
	if orgStr != "" {
		orgUU, err := uuid.FromString(orgStr)
		if err != nil {
			return err
		}
		org, err := db.OrganizationByUUID(ctx, orgUU)
		if err != nil {
			return err
		}
		orgs = append(orgs, *org)
	} else if allOrgs {
		orgs, err = db.AllOrganizations(ctx)
		if err != nil {
			return err
		}
	}

	for _, org := range orgs {
		addSites, err := db.CustomerSitesByOrganization(ctx, org.UUID)
		if err != nil {
			return err
		}
		for _, site := range addSites {
			if site.UUID != appliancedb.NullSiteUUID {
				sites[site.UUID] = site
			}
		}
	}

	if siteStr != "" {
		siteUU, err := uuid.FromString(siteStr)
		if err != nil {
			return err
		}
		site, err := db.CustomerSiteByUUID(ctx, siteUU)
		if err != nil {
			return err
		}
		sites[site.UUID] = *site
	}

	for _, site := range sites {
		classChan := make(chan classification, 10)
		if cmd.Use == "sync" {
			go func() {
				err := sqliteClassifications(ctx, site.UUID, sqliteDB, classChan)
				if err != nil {
					log.Fatalf("failed in sqliteClassifications: %s", err)
				}
				close(classChan)
			}()
		} else {
			// If we're in "clear" mode, then just close the
			// channel, indicating there are no classifications
			// for syncSiteClassifications to read.
			close(classChan)
		}

		syncSiteClassifications(ctx, site.UUID, classChan)
	}
	return nil
}

func deviceIDMain(rootCmd *cobra.Command) {
	deviceIDCmd := &cobra.Command{
		Use:   "deviceid <subcmd> [flags] [args]",
		Short: "Manage cloud device id predictions",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(deviceIDCmd)

	syncDeviceIDCmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync device id predictions from cloud -> site",
		Args:  cobra.NoArgs,
		RunE:  syncDeviceID,
	}
	syncDeviceIDCmd.Flags().StringP("org", "o", "", "sync only for this organization")
	syncDeviceIDCmd.Flags().StringP("site", "s", "", "sync only for this site")
	syncDeviceIDCmd.Flags().BoolP("all", "a", false, "sync for all orgs and all sites")
	syncDeviceIDCmd.Flags().String("sqlite-src", "", "sqlite database containing classifications")
	deviceIDCmd.AddCommand(syncDeviceIDCmd)

	clearDeviceIDCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear device id predictions from indicated site(s)",
		Args:  cobra.NoArgs,
		RunE:  syncDeviceID,
	}
	clearDeviceIDCmd.Flags().StringP("org", "o", "", "clear only for this organization")
	clearDeviceIDCmd.Flags().StringP("site", "s", "", "clear only for this site")
	clearDeviceIDCmd.Flags().BoolP("all", "a", false, "clear for all orgs and all sites")
	deviceIDCmd.AddCommand(clearDeviceIDCmd)
}
