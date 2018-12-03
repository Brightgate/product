/*
 * COPYRIGHT 2017 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or  alteration will be a violation of federal law.
 */

// Usage:
// ap-userctl -add -email jqp@example.com \
//     [-displayName "John Q.  Public"] uid
// ap-userctl -update [-email jqp@example.com] \
//     [-displayName "John Q.  Public"] uid
// ap-userctl -passwd uid
// ap-userctl -set-totp uid
// ap-userctl -clear-totp uid
// ap-userctl -delete uid
// ap-userctl [-v]

package main

import (
	"flag"
	"fmt"
	"log"

	"bg/ap_common/apcfg"
	"bg/common/cfgapi"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	anyStringFlagSet = false
)

// https://stackoverflow.com/questions/35809252/check-if-flag-was-provided-in-go
type stringFlag struct {
	set   bool
	value string
}

func (sf *stringFlag) Set(x string) error {
	sf.value = x
	sf.set = true
	anyStringFlagSet = true
	return nil
}

func (sf *stringFlag) String() string {
	return sf.value
}

var (
	uidArg          string
	displayNameFlag stringFlag
	emailFlag       stringFlag
	phoneFlag       stringFlag
	langFlag        stringFlag
	addOp           bool
	updateOp        bool
	deleteOp        bool
	passwdOp        bool
	setTotpOp       bool
	clearTotpOp     bool
	vFlag           bool
)

func flagInit() {
	flag.BoolVar(&addOp, "add", false, "add a user")
	flag.BoolVar(&updateOp, "update", false, "update a user")
	flag.BoolVar(&deleteOp, "delete", false, "delete a user")
	flag.BoolVar(&passwdOp, "passwd", false, "set a password for user")
	flag.BoolVar(&setTotpOp, "set-totp", false, "set a TOTP for user")
	flag.BoolVar(&clearTotpOp, "clear-totp", false, "clear TOTP for user")
	flag.BoolVar(&vFlag, "v", false, "enable verbose user display")
	flag.Var(&displayNameFlag, "display-name", "displayName value for added user")
	flag.Var(&emailFlag, "email", "email value for added user")
	flag.Var(&phoneFlag, "telephone-number", "telephoneNumber value for added user")
	flag.Var(&langFlag, "language", "preferredLanguage value for added user")

	flag.Parse()
}

var config *cfgapi.Handle

func getUsers() error {
	const getUsersFormat = "%-12s %-8s %-24s %-30s\n"

	users := config.GetUsers()
	if users == nil {
		return fmt.Errorf("Couldn't fetch users")
	}

	fmt.Printf(getUsersFormat,
		"UID", "ROLE", "DISPLAYNAME", "EMAIL")

	for name, user := range users {
		fmt.Printf(getUsersFormat,
			name, user.Role, user.DisplayName, user.Email)
	}
	return nil
}

func printSecret(k, v string) {
	if v != "" {
		v = "[redacted]"
	}
	fmt.Printf("\t%s: %s\n", k, v)
}

func getUsersVerbose() error {
	users := config.GetUsers()
	if users == nil {
		return fmt.Errorf("Couldn't fetch users")
	}

	for name, user := range users {
		fmt.Printf("%s\n", name)
		fmt.Printf("\tUID: %s\n", user.UID)
		fmt.Printf("\tUUID: %s\n", user.UUID)
		fmt.Printf("\tRole: %s\n", user.Role)
		fmt.Printf("\tDisplayName: %s\n", user.DisplayName)
		fmt.Printf("\tEmail: %s\n", user.Email)
		fmt.Printf("\tPreferredLanguage: %s\n", user.PreferredLanguage)
		fmt.Printf("\tTelephoneNumber: %s\n", user.TelephoneNumber)
		printSecret("Password", user.Password)
		printSecret("MD4Password", user.MD4Password)
		printSecret("TOTP", user.TOTP)
		fmt.Printf("\n")
	}
	return nil
}

func addUser() error {
	ui, err := config.NewUserInfo(uidArg)
	if err != nil {
		return err
	}
	ui.DisplayName = displayNameFlag.String()
	ui.Email = emailFlag.String()
	ui.TelephoneNumber = phoneFlag.String()
	ui.PreferredLanguage = langFlag.String()
	err = ui.Update()
	if err == nil {
		log.Printf("added user '%s'\n", uidArg)
	}
	return err
}

func updateUser() error {
	ui, err := config.GetUser(uidArg)
	if err != nil {
		return err
	}
	if !anyStringFlagSet {
		return fmt.Errorf("No changes requested")
	}
	if displayNameFlag.set {
		ui.DisplayName = displayNameFlag.String()
	}
	if emailFlag.set {
		ui.Email = emailFlag.String()
	}
	if phoneFlag.set {
		ui.TelephoneNumber = phoneFlag.String()
	}
	if langFlag.set {
		ui.PreferredLanguage = langFlag.String()
	}
	err = ui.Update()
	if err == nil {
		log.Printf("updated user '%s'\n", uidArg)
	}
	return err
}

func deleteUser() error {
	ui, err := config.GetUser(uidArg)
	if err != nil {
		return err
	}
	err = ui.Delete()
	if err == nil {
		log.Printf("deleted user '%s'\n", uidArg)
	}
	return err
}

func setUserPassword() error {
	const unikey = "\U0001F511"

	ui, err := config.GetUser(uidArg)
	if err != nil {
		return err
	}
	log.Printf("Setting password for user '%s' (%s)\n", uidArg, ui.DisplayName)

	fmt.Print("Enter password: " + unikey)
	ps1, err := terminal.ReadPassword(0)
	fmt.Println("")
	if err != nil {
		return errors.Wrap(err, "could not read password")
	}

	fmt.Print("Reenter password: " + unikey)
	ps2, err := terminal.ReadPassword(0)
	fmt.Println("")
	if err != nil {
		return errors.Wrap(err, "could not read password")
	}

	if string(ps1) != string(ps2) {
		return fmt.Errorf("passwords do not agree")
	}

	err = ui.SetPassword(string(ps1))
	if err != nil {
		return errors.Wrap(err, "SetPassword failed")
	}
	log.Printf("New password set for user '%s'\n", uidArg)

	return nil
}

func setTOTP() error {
	// Get user email.
	ui, err := config.GetUser(uidArg)
	if err != nil {
		return err
	}
	err = ui.CreateTOTP()
	if err != nil {
		return errors.Wrap(err, "Failed to create TOTP secret")
	}
	log.Printf("Created TOTP for <%s> (%s)\n", ui.Email, uidArg)
	return nil
}

func clearTOTP() error {
	ui, err := config.GetUser(uidArg)
	if err != nil {
		return err
	}
	err = ui.ClearTOTP()
	if err != nil {
		return errors.Wrap(err, "Failed to clear TOTP secret")
	}
	log.Printf("Cleared TOTP for <%s> (%s)\n", ui.Email, uidArg)
	return nil
}

func userctl() {
	var err error
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flagInit()

	if addOp || updateOp || deleteOp || passwdOp || setTotpOp || clearTotpOp {
		if flag.NArg() > 1 {
			log.Fatalf("only one user can be specified")
		}
		uidArg = flag.Arg(0)
		if uidArg == "" {
			log.Fatalf("must specify user id")
		}
	} else {
		if flag.NArg() != 0 {
			log.Fatalf("invalid argument for listing")
		}
	}

	config, err = apcfg.NewConfigd(nil, pname, cfgapi.AccessInternal)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	if addOp {
		err = addUser()
	} else if updateOp {
		err = updateUser()
	} else if deleteOp {
		err = deleteUser()
	} else if passwdOp {
		err = setUserPassword()
	} else if setTotpOp {
		err = setTOTP()
	} else if clearTotpOp {
		err = clearTOTP()
	} else if vFlag {
		err = getUsersVerbose()
	} else {
		err = getUsers()
	}
	if err != nil {
		log.Fatalf("Operation failed: %+v", err)
	}
}

func init() {
	addTool("ap-userctl", userctl)
}
