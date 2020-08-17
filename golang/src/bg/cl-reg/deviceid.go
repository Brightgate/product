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
		output <- c
	}
	return nil
}

func syncSiteClassifications(ctx context.Context, siteUUID uuid.UUID, dryRun bool, verbose bool, classifications chan classification) {

	vPrintf := func(fmt string, args ...interface{}) {
		if !verbose {
			return
		}
		log.Printf(fmt, args...)
	}

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
	vPrintf("generating clearouts for classification props")
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
			vPrintf("\t%s: %s", cmac, strings.Join(deletes, ", "))
		}
	}

	vPrintf("ingesting classification records")
	// Consume classification data in the channel ...
	for c := range classifications {
		// ... skip if the client is not present in the tree
		clientPath := fmt.Sprintf("@/clients/%s", c.Mac)
		_, err := cfg.GetProps(clientPath)
		if err != nil {
			if err == cfgapi.ErrNoProp {
				vPrintf("\tskipping client %s; not in tree", clientPath)
				continue
			} else {
				log.Printf("error getting %s; skipping: %v", clientPath, err)
				continue
			}
		}
		propPath := fmt.Sprintf("@/clients/%s/%s", c.Mac, c.PropName)
		oldValue, err := cfg.GetProp(propPath)
		if err != nil && err != cfgapi.ErrNoProp {
			log.Printf("error getting %s; skipping: %v", propPath, err)
			continue
		}
		// ... skip if the old and new values are the same, and shoot
		// down the removal of the property.
		if oldValue == c.Classification {
			delete(macPropOps[c.Mac], c.PropName)
			vPrintf("\tskip %s=%q; already set", c.PropName, c.Classification)
			continue
		}
		// ... else create the property
		vPrintf("\t%s=%q\n", propPath, c.Classification)
		if macPropOps[c.Mac] == nil {
			macPropOps[c.Mac] = make(map[string]cfgapi.PropertyOp)
		}
		macPropOps[c.Mac][c.PropName] = cfgapi.PropertyOp{
			Op:    cfgapi.PropCreate,
			Name:  propPath,
			Value: c.Classification,
		}
	}

	vPrintf("finished ingesting classification records")

	prefix := ""
	if dryRun {
		prefix = "dryrun: "
	}
	// Work through the per-client PropOps.  Generate a PropOp array for
	// each, along with a guarding PropTest (so we don't accidentally
	// recreate a deleted client).  Then execute.
	ops := make([]cfgapi.PropertyOp, 0)
	vPrintf(prefix + "preparing PropOps:")
	for cmac, cPropOpMap := range macPropOps {
		if len(cPropOpMap) == 0 {
			continue
		}
		clientOps := []cfgapi.PropertyOp{
			{
				Op:   cfgapi.PropTest,
				Name: fmt.Sprintf("@/clients/%s", cmac),
			},
		}
		for _, pOp := range cPropOpMap {
			clientOps = append(clientOps, pOp)
		}
		log.Printf("\t%v", clientOps)
		ops = append(ops, clientOps...)
	}

	if len(ops) == 0 {
		log.Printf(prefix+"nothing to sync for %s\n", siteUUID.String())
	} else {
		log.Printf(prefix+"sending %s %d ops", siteUUID.String(), len(ops))
		if !dryRun {
			cmdHdl := cfg.Execute(ctx, ops)
			_, err := cmdHdl.Wait(ctx)
			if err != nil {
				log.Printf("error on cfg execute/wait: %s", err)
				err := cmdHdl.Cancel(ctx)
				if err != nil {
					log.Printf("tried to cancel operation, but cancelation failed: %s", err)
				} else {
					log.Printf("cancelled config operation; site was not responsive")
				}
			}
		}
	}
}

func syncDeviceID(cmd *cobra.Command, args []string) error {
	if environ.ConfigdConnection == "" {
		return fmt.Errorf("Must set B10E_CLREG_CLCONFIGD_CONNECTION")
	}

	orgStr, _ := cmd.Flags().GetString("org")
	siteStr, _ := cmd.Flags().GetString("site")
	allOrgs, _ := cmd.Flags().GetBool("all")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	verbose, _ := cmd.Flags().GetBool("verbose")

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
		obsDSN := fmt.Sprintf("file:%s?cache=shared", obsFile)
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

		log.Printf("--- started syncing <%s> %s", site.Name, site.UUID)
		syncSiteClassifications(ctx, site.UUID, dryRun, verbose, classChan)
		log.Printf("--- finished syncing <%s> %s", site.Name, site.UUID)
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
	syncDeviceIDCmd.Flags().BoolP("dry-run", "n", false, "dry-run-- do not publish changes")
	syncDeviceIDCmd.Flags().BoolP("verbose", "v", false, "extra output")
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
	clearDeviceIDCmd.Flags().BoolP("dry-run", "n", false, "dry-run-- do not publish changes")
	clearDeviceIDCmd.Flags().BoolP("verbose", "v", false, "extra output")
	deviceIDCmd.AddCommand(clearDeviceIDCmd)
}

