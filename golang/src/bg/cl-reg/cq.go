/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/satori/uuid"
	"github.com/spf13/cobra"
	"github.com/tatsushid/go-prettytable"

	"bg/cloud_models/appliancedb"
	"bg/common/cfgmsg"
)

const (
	timeLayout = "2006-01-02 15:04:05.000 MST"
)

func listCq(cmd *cobra.Command, args []string) error {
	uStr, _ := cmd.Flags().GetString("uuid")

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	var u uuid.NullUUID
	if uStr != "" {
		uu, err := uuid.FromString(uStr)
		if err != nil {
			return err
		}
		u.UUID = uu
		u.Valid = true
	}

	cmds, err := db.CommandAudit(context.Background(), u, 0, math.MaxUint32)
	if err != nil {
		return err
	}

	// XXX time between sent and done? nsent?
	table, _ := prettytable.NewTable(
		prettytable.Column{Header: "ID"},
		prettytable.Column{Header: "Site UUID"},
		prettytable.Column{Header: "State"},
		prettytable.Column{Header: "Time"},
		prettytable.Column{Header: "Query Length", AlignRight: true},
		prettytable.Column{Header: "Response Length", AlignRight: true},
	)
	table.Separator = "  "

	for _, cmd := range cmds {
		var ts time.Time
		if cmd.DoneTime.Valid {
			ts = cmd.DoneTime.Time
		} else if cmd.SentTime.Valid {
			ts = cmd.SentTime.Time
		} else {
			ts = cmd.EnqueuedTime
		}
		table.AddRow(cmd.ID, cmd.UUID, cmd.State,
			ts.In(time.Local).Round(time.Millisecond).Format(timeLayout),
			len(cmd.Query), len(cmd.Response))
	}
	table.Print()

	return nil
}

func statusCq(cmd *cobra.Command, args []string) error {
	uStr, _ := cmd.Flags().GetString("uuid")
	wide, _ := cmd.Flags().GetBool("wide")

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	var u uuid.NullUUID
	if uStr != "" {
		uu, err := uuid.FromString(uStr)
		if err != nil {
			return err
		}
		u.UUID = uu
		u.Valid = true
	}

	cmds, err := db.CommandAudit(context.Background(), u, 0, math.MaxUint32)
	if err != nil {
		return err
	}

	m := make(map[uuid.UUID][]*appliancedb.SiteCommand)
	totals := make(map[string]int)
	for _, cmd := range cmds {
		m[cmd.UUID] = append(m[cmd.UUID], cmd)
		totals[cmd.State]++
	}

	columns := []prettytable.Column{prettytable.Column{Header: "Site UUID"}}
	// XXX timing stats? nsent? histograms?
	if wide {
		columns = append(columns,
			prettytable.Column{Header: "#Done", AlignRight: true},
			prettytable.Column{Header: "First Done"},
			prettytable.Column{Header: "Last Done"},
			prettytable.Column{Header: "#Work", AlignRight: true},
			prettytable.Column{Header: "First Work"},
			prettytable.Column{Header: "Last Work"},
			prettytable.Column{Header: "#Enqd", AlignRight: true},
			prettytable.Column{Header: "First Enqd"},
			prettytable.Column{Header: "Last Enqd"})
	} else {
		columns = append(columns,
			prettytable.Column{Header: "State"},
			prettytable.Column{Header: "#", AlignRight: true},
			prettytable.Column{Header: "First"},
			prettytable.Column{Header: "Last"})
	}
	table, _ := prettytable.NewTable(columns...)
	table.Separator = "  "

	// Order the output lexically by UUID just for some sort of stable
	// ordering.
	uuids := make([]uuid.UUID, len(m))
	var i int
	for uu := range m {
		uuids[i] = uu
		i++
	}
	sort.Slice(uuids, func(i, j int) bool {
		return bytes.Compare(uuids[i].Bytes(), uuids[j].Bytes()) == -1
	})

	for _, uu := range uuids {
		cmds = m[uu]
		byState := make(map[string][]*appliancedb.SiteCommand)
		for _, cmd := range cmds {
			byState[cmd.State] = append(byState[cmd.State], cmd)
		}
		timeSort := func(state string) func(i, j int) bool {
			return func(i, j int) bool {
				slice := byState[state]
				switch state {
				case "DONE":
					return slice[i].DoneTime.Time.Before(slice[j].DoneTime.Time)
				case "WORK":
					return slice[i].SentTime.Time.Before(slice[j].SentTime.Time)
				case "ENQD":
					return slice[i].EnqueuedTime.Before(slice[j].EnqueuedTime)
				}
				return false
			}
		}
		sort.Slice(byState["DONE"], timeSort("DONE"))
		sort.Slice(byState["WORK"], timeSort("WORK"))
		sort.Slice(byState["ENQD"], timeSort("ENQD"))
		doneLen := len(byState["DONE"])
		workLen := len(byState["WORK"])
		enqdLen := len(byState["ENQD"])

		var firstDone, lastDone, firstWork, lastWork, firstEnqd, lastEnqd string
		if doneLen > 0 {
			firstDone = byState["DONE"][0].DoneTime.Time.In(time.Local).Format(timeLayout)
			lastDone = byState["DONE"][doneLen-1].DoneTime.Time.In(time.Local).Format(timeLayout)
		}
		if workLen > 0 {
			firstWork = byState["WORK"][0].SentTime.Time.In(time.Local).Format(timeLayout)
			lastWork = byState["WORK"][workLen-1].SentTime.Time.In(time.Local).Format(timeLayout)
		}
		if enqdLen > 0 {
			firstEnqd = byState["ENQD"][0].EnqueuedTime.In(time.Local).Format(timeLayout)
			lastEnqd = byState["ENQD"][enqdLen-1].EnqueuedTime.In(time.Local).Format(timeLayout)
		}
		if wide {
			table.AddRow(uu, doneLen, firstDone, lastDone,
				workLen, firstWork, lastWork,
				enqdLen, firstEnqd, lastEnqd)
		} else {
			table.AddRow(uu, "DONE", doneLen, firstDone, lastDone)
			table.AddRow("", "WORK", workLen, firstWork, lastWork)
			table.AddRow("", "ENQD", enqdLen, firstEnqd, lastEnqd)
		}
	}
	table.Print()

	fmt.Printf("Totals: Done: %d  Canceled: %d  Working: %d  Enqueued: %d\n",
		totals["DONE"], totals["CNCL"], totals["WORK"], totals["ENQD"])

	return nil
}

func getCq(cmd *cobra.Command, args []string) error {
	cmdIDStr := args[0]

	// -q and -r are independent boolean flags that need to be treated as if
	// they were correlated.  -r is set to true by default, and -q to false.
	// If we put -q on the commandline, then that should override -r's
	// default unless -r is also on the commandline.  It is an error to set
	// them both explicitly either to true or false.
	queryFlag := cmd.Flags().Lookup("query")
	responseFlag := cmd.Flags().Lookup("response")
	showQuery, _ := cmd.Flags().GetBool("query")
	showResponse, _ := cmd.Flags().GetBool("response")

	showQuery = showQuery && queryFlag.Changed
	showResponse = showResponse && (!queryFlag.Changed || responseFlag.Changed)

	if showQuery && showResponse {
		return requiredUsage{
			cmd:         cmd,
			msg:         "Invalid flag combination",
			explanation: "The --query and --response flags must not both be set.\n",
		}
	}
	if !showQuery && !showResponse {
		return requiredUsage{
			cmd:         cmd,
			msg:         "Invalid flag combination",
			explanation: "One of the --query or --response flags must be set.\n",
		}
	}

	cmdID, err := strconv.Atoi(cmdIDStr)
	if err != nil {
		return err
	}

	db, _, err := assembleRegistry(cmd)
	if err != nil {
		return err
	}

	cqCmds, err := db.CommandAudit(context.Background(), uuid.NullUUID{}, int64(cmdID-1), 1)
	if err != nil {
		return err
	}
	if len(cqCmds) == 0 {
		return fmt.Errorf("Command not found")
	}
	cqCmd := cqCmds[0]

	if showQuery {
		fmt.Println(string(cqCmd.Query))
	} else {
		showValue, _ := cmd.Flags().GetBool("value")
		if showValue {
			var response cfgmsg.ConfigResponse
			json.Unmarshal(cqCmd.Response, &response)
			fmt.Println(response.Value)
		} else {
			fmt.Println(string(cqCmd.Response))
		}
	}

	return nil
}

func cqMain(rootCmd *cobra.Command) {
	cqCmd := &cobra.Command{
		Use:   "cq <subcmd> [flags] [args]",
		Short: "Administer command queue",
		Args:  cobra.NoArgs,
	}
	rootCmd.AddCommand(cqCmd)

	listCqCmd := &cobra.Command{
		Use:     "list [flags]",
		Args:    cobra.NoArgs,
		Short:   "List commands in the queue",
		Aliases: []string{"ls"},
		RunE:    listCq,
	}
	listCqCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	listCqCmd.Flags().StringP("uuid", "u", "", "appliance UUID")
	cqCmd.AddCommand(listCqCmd)

	statusCqCmd := &cobra.Command{
		Use:   "status [flags]",
		Args:  cobra.NoArgs,
		Short: "Status of commands in the queue",
		RunE:  statusCq,
	}
	statusCqCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	statusCqCmd.Flags().StringP("uuid", "u", "", "appliance UUID")
	statusCqCmd.Flags().BoolP("wide", "w", false, "wide display")
	cqCmd.AddCommand(statusCqCmd)

	// "get" subcommand that returns either the query or the response for a
	// particular command ID.
	getCqCmd := &cobra.Command{
		Use:   "get [flags]",
		Args:  cobra.ExactArgs(1),
		Short: "Retrieve query or response blobs",
		RunE:  getCq,
	}
	getCqCmd.Flags().StringP("input", "i", "", "registry data JSON file")
	getCqCmd.Flags().BoolP("query", "q", false, "retrieve query")
	getCqCmd.Flags().BoolP("response", "r", true, "retrieve response")
	getCqCmd.Flags().BoolP("value", "v", false, "emit response value")
	cqCmd.AddCommand(getCqCmd)
}

