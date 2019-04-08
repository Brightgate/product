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
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"
	"github.com/tomazk/envcfg"

	"bg/cl_common/pgutils"
	"bg/cl_common/registry"
	"bg/cloud_models/appliancedb"
)

const pname = "cl-reg"

var environ struct {
	PostgresConnection string `envcfg:"REG_DBURI"`
	Project            string `envcfg:"REG_PROJECT_ID"`
	Region             string `envcfg:"REG_REGION_ID"`
	Registry           string `envcfg:"REG_REGISTRY_ID"`
	ConfigdConnection  string `envcfg:"B10E_CLREG_CLCONFIGD_CONNECTION"`
	DisableTLS         bool   `envcfg:"B10E_CLREG_DISABLE_TLS"`
	AccountSecret      string `envcfg:"B10E_CLREG_ACCOUNT_SECRET"`
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

func assembleRegistry(cmd *cobra.Command) (appliancedb.DataStore, *registry.ApplianceRegistry, error) {
	var reg registry.ApplianceRegistry
	project, _ := cmd.Flags().GetString("project")
	region, _ := cmd.Flags().GetString("region")
	regID, _ := cmd.Flags().GetString("registry")
	inputPath, _ := cmd.Flags().GetString("input")

	fileReg, err := readJSON(inputPath)
	if err != nil {
		return nil, nil, err
	}
	// This means there is no way to override a non-empty parameter from the
	// environment or the JSON file with, say, `-p ""`.
	reg.Project = first(project, fileReg.Project, environ.Project)
	reg.Region = first(region, fileReg.Region, environ.Region)
	reg.Registry = first(regID, fileReg.Registry, environ.Registry)

	pgconn := first(fileReg.DbURI, environ.PostgresConnection)
	if pgconn == "" {
		return nil, nil, requiredUsage{
			cmd: cmd,
			msg: "Missing database URI",
			explanation: "You must provide the registry database URI through the environment\n" +
				"variable REG_DBURI or via the JSON file specified with -i.\n",
		}
	}
	pgconn, err = pgutils.PasswordPrompt(pgconn)
	if err != nil {
		return nil, nil, err
	}
	reg.DbURI = pgconn
	db, err := appliancedb.Connect(reg.DbURI)
	if err != nil {
		return nil, nil, err
	}
	return db, &reg, nil
}

func main() {
	rootCmd := &cobra.Command{
		Use:              os.Args[0],
		PersistentPreRun: silenceUsage,
	}

	accountMain(rootCmd)
	appMain(rootCmd)
	cqMain(rootCmd)
	oauth2Main(rootCmd)
	orgMain(rootCmd)
	siteMain(rootCmd)

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
