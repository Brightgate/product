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
// ap-userctl

package main

import (
	"flag"
	"fmt"
	"log"
	"net/mail"
	"time"

	"bg/ap_common/apcfg"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/pquerna/otp/totp"
	"github.com/satori/uuid"
	"github.com/ttacon/libphonenumber"
)

type setfunc func(string, string, *time.Time) error

const (
	pname      = "ap-userctl"
	totpIssuer = "brightgate-userctl"
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
)

var config *apcfg.APConfig

const (
	getUsersHeaders = "%s %s %s %s %s\n"
	getUsersFormat  = "%s %s %s %s %s\n"
)

func getUsers() {
	users := config.GetUsers()

	fmt.Printf(getUsersHeaders,
		"uid", "displayName", "email", "telephoneNumber", "2fa?")

	for name, user := range users {
		t := "-"
		if user.TOTP != "" {
			t = "yes"
		}

		fmt.Printf(getUsersFormat,
			name, user.DisplayName, user.Email, user.TelephoneNumber, t)
	}
}

func addUser(u string, dn string, email string, pn string, lang string) {
	_, err := mail.ParseAddress(email)
	if err != nil {
		log.Fatalf("email must be legitimate RFC5322 address: %v\n", err)
	}
	if email == "" {
		log.Fatalf("email must be non-empty\n")
	}

	phone, err := libphonenumber.Parse(pn, "US")
	if err != nil {
		log.Fatalf("phoneNumber must be parseable by libphonenumber: %v\n", err)
	}

	log.Printf("add user '%s'\n", u)

	if pn != "" {
		config.CreateProp(
			fmt.Sprintf("@/users/%s/telephoneNumber", u),
			libphonenumber.Format(phone, libphonenumber.INTERNATIONAL),
			nil)
	}

	config.CreateProp(fmt.Sprintf("@/users/%s/uid", u), u, nil)
	config.CreateProp(fmt.Sprintf("@/users/%s/email", u), email, nil)
	config.CreateProp(fmt.Sprintf("@/users/%s/uuid", u), uuid.NewV4().String(), nil)

	// XXX Tests for valid displayName?
	if dn != "" {
		config.CreateProp(fmt.Sprintf("@/users/%s/displayName", u), dn,
			nil)
	}

	// XXX Tests for valid preferredLanguage?
	if lang != "" {
		config.CreateProp(
			fmt.Sprintf("@/users/%s/preferredLanguage", u), lang,
			nil)
	}
}

func deleteUser(u string) {
	if u == "" {
		log.Fatalf("ignoring deletion of empty user ID\n")
	}

	log.Printf("delete user '%s'\n", u)
	ut := fmt.Sprintf("@/users/%s", u)
	err := config.DeleteProp(ut)
	if err != nil {
		log.Fatalf("could not delete user '%v': %v\n", u, err)
	}
}

const unikey = "\U0001F511"

func setUserPassword(u string) error {
	log.Printf("set password for user '%s'\n", u)

	fmt.Print("Enter password: " + unikey)
	ps1, err := terminal.ReadPassword(0)
	fmt.Println("")
	if err != nil {
		return fmt.Errorf("could not read password: %v", err)
	}

	fmt.Print("Reenter password: " + unikey)
	ps2, err := terminal.ReadPassword(0)
	fmt.Println("")
	if err != nil {
		return fmt.Errorf("could not read password: %v", err)
	}

	if string(ps1) != string(ps2) {
		return fmt.Errorf("passwords do not agree")
	}

	err = config.SetUserPassword(u, string(ps1))
	if err != nil {
		return fmt.Errorf("SetUserPassword failed: %v", err)
	}

	return nil
}

func setTOTP(u string) {
	// Get user email.
	ui, err := config.GetUser(u)
	if err != nil {
		log.Fatalf("cannot load user '%s'\n", u)
	}

	var accountName string

	if ui.Email != "" {
		accountName = ui.Email
	} else {
		// Suboptimal, as potentially collision prone.
		accountName = u
	}

	log.Printf("set TOTP for user '%s'\n", u)
	totpgen, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: accountName,
	})
	if err != nil {
		log.Fatalf("TOTP generation failed: %v\n", err)
	}

	config.CreateProp(fmt.Sprintf("@/users/%s/totp", u), totpgen.String(), nil)
}

func clearTOTP(u string) {
	log.Printf("clear TOTP for user '%s'\n", u)
	utotp := fmt.Sprintf("@/users/%s/totp", u)
	err := config.DeleteProp(utotp)
	if err != nil {
		log.Fatalf("TOTP property deletion failed: %v\n", err)
	}
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
		log.Printf("ignoring users after first\n")
	}

	if *addOp {
		if len(flag.Arg(0)) == 0 {
			log.Fatalf("Must provide a username\n")
		}
		addUser(flag.Arg(0), *displayNameArg, *emailArg, *phoneArg,
			*langArg)
	} else if *deleteOp {
		deleteUser(flag.Arg(0))
	} else if *passwdOp {
		setUserPassword(flag.Arg(0))
	} else if *setTotpOp {
		setTOTP(flag.Arg(0))
	} else if *clearTotpOp {
		clearTOTP(flag.Arg(0))
	} else {
		getUsers()
	}
}
