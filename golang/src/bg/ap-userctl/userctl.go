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
// ap-userctl -passwd uid
// ap-userctl -set-totp uid
// ap-userctl -clear-totp uid
// ap-userctl -delete uid
// ap-userctl uid
// ap-userctl [-v]

package main

import (
	"flag"
	"fmt"
	"log"

	"bg/ap_common/apcfg"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh/terminal"
)

const (
	pname = "ap-userctl"
)

var (
	addOp          = flag.Bool("add", false, "add a user")
	deleteOp       = flag.Bool("delete", false, "delete a user")
	passwdOp       = flag.Bool("passwd", false, "set a password for user")
	setTotpOp      = flag.Bool("set-totp", false, "set a TOTP for user")
	clearTotpOp    = flag.Bool("clear-totp", false, "clear TOTP for user")
	displayNameArg = flag.String("display-name", "",
		"displayName value for added user")
	emailArg = flag.String("email", "", "email value for added user")
	phoneArg = flag.String("telephone-number", "",
		"telephoneNumber value for added user")
	langArg = flag.String("language", "en",
		"preferredLanguage value for added user")
	vFlag = flag.Bool("v", false, "enable verbose user display")
)

var config *apcfg.APConfig

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

func addUser(uid, displayName, email, phone, lang string) error {
	err := config.AddUser(uid, displayName, email, phone, lang)
	if err == nil {
		log.Printf("added user '%s'\n", uid)
	}
	return err
}

func deleteUser(u string) error {
	err := config.DeleteUser(u)
	if err == nil {
		log.Printf("deleted user '%s'\n", u)
	}
	return err
}

func getUser(u string) (*apcfg.UserInfo, error) {
	ui, err := config.GetUser(u)
	return ui, err
}

func setUserPassword(u string) error {
	const unikey = "\U0001F511"

	ui, err := getUser(u)
	if err != nil {
		return err
	}
	log.Printf("Setting password for user '%s' (%s)\n", u, ui.DisplayName)

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
	log.Printf("New password set for user '%s'\n", u)

	return nil
}

func setTOTP(u string) error {
	// Get user email.
	ui, err := getUser(u)
	if err != nil {
		return err
	}
	err = ui.CreateTOTP()
	if err != nil {
		return errors.Wrap(err, "Failed to create TOTP secret")
	}
	log.Printf("Created TOTP for <%s> (%s)\n", ui.Email, u)
	return nil
}

func clearTOTP(u string) error {
	ui, err := getUser(u)
	if err != nil {
		return err
	}
	err = ui.ClearTOTP()
	if err != nil {
		return errors.Wrap(err, "Failed to clear TOTP secret")
	}
	log.Printf("Cleared TOTP for <%s> (%s)\n", ui.Email, u)
	return nil
}

func main() {
	var err error

	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	flag.Parse()

	config, err = apcfg.NewConfig(nil, pname)
	if err != nil {
		log.Fatalf("cannot connect to configd: %v\n", err)
	}

	if flag.NArg() > 1 {
		log.Fatalf("only one user can be specified")
	}

	if *addOp {
		err = addUser(flag.Arg(0), *displayNameArg, *emailArg, *phoneArg,
			*langArg)
	} else if *deleteOp {
		err = deleteUser(flag.Arg(0))
	} else if *passwdOp {
		err = setUserPassword(flag.Arg(0))
	} else if *setTotpOp {
		err = setTOTP(flag.Arg(0))
	} else if *clearTotpOp {
		err = clearTOTP(flag.Arg(0))
	} else if *vFlag {
		err = getUsersVerbose()
	} else {
		err = getUsers()
	}
	if err != nil {
		log.Fatalf("Operation failed: %+v", err)
	}
}
