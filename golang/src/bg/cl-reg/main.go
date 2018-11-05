/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"syscall"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"
	"github.com/tomazk/envcfg"
	"golang.org/x/crypto/ssh/terminal"

	"bg/cl_common/pgutils"
	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
)

var environ struct {
	PostgresConnection string `envcfg:"REG_DBURI"`
	Project            string `envcfg:"REG_PROJECT_ID"`
	Region             string `envcfg:"REG_REGION_ID"`
	Registry           string `envcfg:"REG_REGISTRY_ID"`
}

type requiredUsage struct {
	cmd         *cobra.Command
	msg         string
	explanation string
}

func (e requiredUsage) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "More information needed"
}

func silenceUsage(cmd *cobra.Command, args []string) {
	// If we set this when creating cmd, then if cobra fails argument
	// validation, it doesn't emit the usage, but if we leave it alone, we
	// get a usage message on all errors.  Here, we set it after all the
	// argument validation, and we get a usage message only on argument
	// validation failure.
	// See https://github.com/spf13/cobra/issues/340#issuecomment-378726225
	cmd.SilenceUsage = true
}

func passwordPrompt(dbURI string) (string, error) {
	if !pgutils.HasPassword(dbURI) {
		fmt.Print("Enter DB password: ")
		bytePassword, err := terminal.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			return "", err
		}
		dbURI = pgutils.AddPassword(dbURI, string(bytePassword))
	}
	return dbURI, nil
}

func readJSON(path string) (*registry.ApplianceRegistry, error) {
	var reg registry.ApplianceRegistry
	if path == "" {
		return &reg, nil
	}

	jblob, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(jblob, &reg)
	if err != nil {
		return nil, err
	}

	return &reg, nil
}

func first(opts ...string) string {
	for _, opt := range opts {
		if opt != "" {
			return opt
		}
	}
	return ""
}

func listAppliances(cmd *cobra.Command, args []string) error {
	project, _ := cmd.Flags().GetString("project")
	region, _ := cmd.Flags().GetString("region")
	regID, _ := cmd.Flags().GetString("registry")
	appID, _ := cmd.Flags().GetString("name")
	inputPath, _ := cmd.Flags().GetString("input")

	reg, err := readJSON(inputPath)
	if err != nil {
		return err
	}

	pgconn := first(reg.DbURI, environ.PostgresConnection)
	if pgconn == "" {
		return requiredUsage{
			cmd: cmd,
			msg: "Missing database URI",
			explanation: "You must provide the registry database URI through the environment\n" +
				"variable REG_DBURI or via the JSON file specified with -i.\n",
		}
	}

	// This means there is no way to override a non-empty parameter from the
	// environment or the JSON file with, say, `-p ""`.
	project = first(project, reg.Project, environ.Project)
	region = first(region, reg.Region, environ.Region)
	regID = first(regID, reg.Registry, environ.Registry)

	dbURI, err := passwordPrompt(pgconn)
	if err != nil {
		return err
	}
	db, err := appliancedb.Connect(dbURI)
	if err != nil {
		return err
	}
	defer db.Close()

	apps, err := db.AllApplianceIDs(context.Background())
	if err != nil {
		return err
	}

	// XXX We could write a query with a WHERE clause ...
	// XXX It might also be nice to have pattern matching.
	// XXX And sorting
	matchingApps := make([]appliancedb.ApplianceID, 0)
	for _, app := range apps {
		if (project == "" || project == app.GCPProject) &&
			(region == "" || region == app.GCPRegion) &&
			(regID == "" || regID == app.ApplianceReg) &&
			(appID == "" || appID == app.ApplianceRegID) {
			matchingApps = append(matchingApps, app)
		}
	}

	if len(matchingApps) == 0 {
		return nil
	}

	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "UUID"},
		prettytable.Column{Header: "Project"},
		prettytable.Column{Header: "Region"},
		prettytable.Column{Header: "Registry"},
		prettytable.Column{Header: "Appliance Name"},
	)
	table.Separator = "  "

	for _, app := range matchingApps {
		table.AddRow(app.CloudUUID, app.GCPProject, app.GCPRegion,
			app.ApplianceReg, app.ApplianceRegID)
	}
	table.Print()
	return nil
}

func newAppliance(cmd *cobra.Command, args []string) error {
	appID := args[0]
	project, _ := cmd.Flags().GetString("project")
	region, _ := cmd.Flags().GetString("region")
	regID, _ := cmd.Flags().GetString("registry")
	outdir, _ := cmd.Flags().GetString("directory")
	inputPath, _ := cmd.Flags().GetString("input")
	appUUID, _ := cmd.Flags().GetString("uuid")

	var u uuid.UUID
	if appUUID != "" {
		var err error
		if u, err = uuid.FromString(appUUID); err != nil {
			return err
		}
	}

	reg, err := readJSON(inputPath)
	if err != nil {
		return err
	}

	pgconn := first(reg.DbURI, environ.PostgresConnection)
	project = first(project, reg.Project, environ.Project)
	region = first(region, reg.Region, environ.Region)
	regID = first(regID, reg.Registry, environ.Registry)
	if project == "" || region == "" || regID == "" || pgconn == "" {
		return requiredUsage{
			cmd: cmd,
			explanation: "The GCP project and region, and appliance registry name must be\n" +
				"provided, along with the registry database URI.  You can do this\n" +
				"with command-line arguments, through environment variables:\n" +
				"    REG_PROJECT_ID=<name of gcp project>\n" +
				"    REG_REGION_ID=<name of gcp region>\n" +
				"    REG_REGISTRY_ID=<registry name>\n" +
				"    REG_DBURI=<postgres uri>\n" +
				"or through a JSON file specified with the -i flag.\n",
		}
	}

	dbURI, err := passwordPrompt(pgconn)
	if err != nil {
		return err
	}
	db, err := appliancedb.Connect(dbURI)
	if err != nil {
		return err
	}
	defer db.Close()

	var keyPEM []byte
	if appUUID == "" {
		u, keyPEM, _, err = registry.NewAppliance(context.Background(), db,
			project, region, regID, appID)
	} else {
		keyPEM, _, err = registry.NewApplianceWithUUID(context.Background(),
			db, u, project, region, regID, appID)
	}
	if err != nil {
		return err
	}

	jmap := map[string]string{
		"project":      project,
		"region":       region,
		"registry":     regID,
		"appliance_id": appID,
		"private_key":  string(keyPEM),
	}
	jout, err := json.MarshalIndent(jmap, "", "\t")
	if err != nil {
		return err
	}

	var ioerr error
	var secretsFile string
	if ioerr = os.MkdirAll(outdir, 0700); ioerr == nil {
		secretsFile = outdir + "/" + appID + ".cloud.secret.json"
		ioerr = ioutil.WriteFile(secretsFile, jout, 0600)
	}

	fmt.Printf("-------------------------------------------------------------\n")
	fmt.Printf("      Created device: projects/%s/locations/%s/registries/%s/appliances/%s\n",
		project, region, regID, appID)
	fmt.Printf("Appliance Cloud UUID: %s\n", u)
	if ioerr == nil {
		fmt.Printf("        Secrets file: %s\n", secretsFile)
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("Next, provision %s to the appliance at:\n", secretsFile)
		fmt.Printf("    /opt/com.brightgate/etc/secret/cloud/cloud.secret.json\n")
	} else {
		fmt.Printf("-------------------------------------------------------------\n")
		fmt.Printf("Secrets file couldn't be written: %s\n", ioerr)
		fmt.Printf("Copy the following to the appliance at:\n")
		fmt.Printf("    /opt/com.brightgate/etc/secret/cloud/cloud.secret.json\n")
		fmt.Printf("%s\n", jout)
	}

	return err
}

func main() {
	rootCmd := cobra.Command{
		Use:              os.Args[0],
		PersistentPreRun: silenceUsage,
	}

	appCmd := &cobra.Command{
		Use:   "app <subcmd> [flags] [args]",
		Short: "Administer appliances in the registry",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(appCmd)

	newAppCmd := &cobra.Command{
		Use:   "new [flags] <appliance name>",
		Args:  cobra.ExactArgs(1),
		Short: "Create an appliance and add it to the registry",
		RunE:  newAppliance,
	}
	newAppCmd.Flags().StringP("directory", "d", ".", "output directory")
	newAppCmd.Flags().StringP("project", "p", "", "GCP project")
	newAppCmd.Flags().StringP("region", "R", "", "GCP region")
	newAppCmd.Flags().StringP("registry", "r", "", "appliance registry")
	newAppCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	newAppCmd.Flags().StringP("uuid", "u", "", "appliance UUID")
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
	appCmd.AddCommand(listAppCmd)

	if err := envcfg.Unmarshal(&environ); err != nil {
		fmt.Printf("Environment Error: %s", err)
		return
	}

	err := rootCmd.Execute()
	if err, ok := err.(requiredUsage); ok {
		err.cmd.Usage()
		extraUsage := "\n" + err.explanation
		io.WriteString(err.cmd.OutOrStderr(), extraUsage)
	}
	os.Exit(map[bool]int{true: 0, false: 1}[err == nil])
}
