/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package apcfg

import (
	"fmt"
	"log"
	"net/mail"

	"github.com/pkg/errors"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/md4"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/pquerna/otp/totp"
	"github.com/satori/uuid"
	"github.com/ttacon/libphonenumber"
)

const (
	totpIssuer = "brightgate.com"
)

// UserInfo contains all of the configuration information for an appliance user
// account.  Expected roles are: "SITE_ADMIN", "SITE_USER",
// "SITE_GUEST", "CUST_ADMIN", "CUST_USER", "CUST_GUEST".
type UserInfo struct {
	UID               string // Username
	UUID              string
	Role              string // User role
	DisplayName       string // User's friendly name
	Email             string // User email
	PreferredLanguage string
	TelephoneNumber   string // User telephone number
	TOTP              string // Time-based One Time Password URL
	Password          string // bcrypt Password
	MD4Password       string // MD4 Password for WPA-EAP/MSCHAPv2
	config            *APConfig
}

// UserMap maps an account's username to its configuration information
type UserMap map[string]*UserInfo

// newUserFromNode creates a UserInfo from config properties
func newUserFromNode(user *PropertyNode) (*UserInfo, error) {
	uid, err := getStringVal(user, "uid")
	if err != nil {
		// Most likely manual creation of the @/users/[uid] node.
		return nil, errors.Wrap(err, "incomplete user property node")
	}
	if user.Name != uid {
		return nil, fmt.Errorf("prop name '%s' != uid '%s'", user.Name, uid)
	}

	password, _ := getStringVal(user, "userPassword")
	md4password, _ := getStringVal(user, "userMD4Password")
	uuid, _ := getStringVal(user, "uuid")
	email, _ := getStringVal(user, "email")
	telephoneNumber, _ := getStringVal(user, "telephoneNumber")
	preferredLanguage, _ := getStringVal(user, "preferredLanguage")
	displayName, _ := getStringVal(user, "displayName")
	totp, _ := getStringVal(user, "totp")

	u := &UserInfo{
		UID:               uid,
		UUID:              uuid,
		Email:             email,
		TelephoneNumber:   telephoneNumber,
		PreferredLanguage: preferredLanguage,
		DisplayName:       displayName,
		TOTP:              totp,
		Password:          password,
		MD4Password:       md4password,
	}

	return u, nil
}

// AddUser creates a new user record in the config store for the given user
// if the user already exists, their record is modified.
func (c *APConfig) AddUser(u, displayName, email, phone, lang string) error {
	// XXX limits on length?
	if u == "" {
		return fmt.Errorf("user name (uid) must be supplied")
	}
	if email == "" {
		return fmt.Errorf("email must be supplied")
	}
	if phone == "" {
		return fmt.Errorf("telephone number must be supplied")
	}

	_, err := mail.ParseAddress(email)
	if err != nil {
		return errors.Wrap(err, "email must be legitimate RFC5322 address: %s")
	}
	phoneNum, err := libphonenumber.Parse(phone, "US")
	if err != nil {
		return errors.Wrap(err, "invalid phoneNumber")
	}

	_ = c.CreateProp(fmt.Sprintf("@/users/%s/uid", u), u, nil)
	_ = c.CreateProp(fmt.Sprintf("@/users/%s/email", u), email, nil)
	_ = c.CreateProp(fmt.Sprintf("@/users/%s/uuid", u), uuid.NewV4().String(), nil)

	// XXX Tests for valid displayName?
	if displayName != "" {
		_ = c.CreateProp(fmt.Sprintf("@/users/%s/displayName", u), displayName,
			nil)
	}

	if phone != "" {
		_ = c.CreateProp(
			fmt.Sprintf("@/users/%s/telephoneNumber", u),
			libphonenumber.Format(phoneNum, libphonenumber.INTERNATIONAL),
			nil)
	}

	// XXX Tests for valid preferredLanguage?
	if lang != "" {
		_ = c.CreateProp(
			fmt.Sprintf("@/users/%s/preferredLanguage", u), lang,
			nil)
	}

	return nil
}

// DeleteUser removes a new user record from the config store for the given uid
func (c *APConfig) DeleteUser(uid string) error {
	ut := fmt.Sprintf("@/users/%s", uid)
	err := c.DeleteProp(ut)
	if err != nil {
		return errors.Wrapf(err, "could not delete user '%s'", uid)
	}
	return nil
}

// GetUser fetches the UserInfo structure for a given user
func (c *APConfig) GetUser(uid string) (*UserInfo, error) {
	if uid == "" {
		return nil, fmt.Errorf("uid must be specified")
	}
	user, err := c.GetProps("@/users/" + uid)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get user %s", uid)
	}
	ui, err := newUserFromNode(user)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to make UserInfo")
	}
	ui.config = c
	return ui, nil
}

// GetUserByUUID fetches the UserInfo structure for a given UUID
func (c *APConfig) GetUserByUUID(uuid string) (*UserInfo, error) {
	if uuid == "" {
		return nil, fmt.Errorf("uuid must be specified")
	}
	users, err := c.GetProps("@/users/")
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get user list")
	}
	for _, user := range users.Children {
		upn := user.GetChild("uuid")
		if upn == nil {
			continue
		}
		if upn.Value == uuid {
			ui, err := newUserFromNode(user)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to make UserInfo")
			}
			ui.config = c
			return ui, nil
		}
	}
	return nil, errors.Errorf("No user with UUID %s", uuid)
}

// GetUsers fetches the Users subtree, in the form of a map of UID to UserInfo
// structures.
func (c *APConfig) GetUsers() UserMap {
	props, err := c.GetProps("@/users")
	if err != nil {
		return nil
	}

	set := make(map[string]*UserInfo)
	for _, user := range props.Children {
		if us, err := newUserFromNode(user); err == nil {
			us.config = c
			set[user.Name] = us
		} else {
			// XXX kludge
			log.Printf("couldn't userinfo %v: %v\n", user.Name, err)
		}
	}

	return set
}

func (u *UserInfo) path(comp string) string {
	p := fmt.Sprintf("@/users/%s", u.UID)
	if comp != "" {
		p += "/" + comp
	}
	return p
}

// SetPassword assigns all appropriate password hash properties for the given user.
func (u *UserInfo) SetPassword(passwd string) error {
	// Generate bcrypt password property.
	hps, err := bcrypt.GenerateFromPassword([]byte(passwd), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not encrypt password: %v", err)
	}

	err = u.config.CreateProp(u.path("userPassword"), string(hps), nil)
	if err != nil {
		return errors.Wrapf(err,
			"could not create userPassword property for %s", u.UID)
	}

	// Generate MD4 password property.
	enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
	md4ps := md4.New()
	t := transform.NewWriter(md4ps, enc)
	_, err = t.Write([]byte(passwd))
	if err != nil {
		return errors.Wrapf(err, "could not write MD4 for %s", u.UID)
	}
	md4s := fmt.Sprintf("%x", md4ps.Sum(nil))

	err = u.config.CreateProp(u.path("userMD4Password"), md4s, nil)
	if err != nil {
		return errors.Wrapf(err,
			"could not create userMD4Password property for %s", u.UID)
	}

	return nil
}

// CreateTOTP generates a time-based one time password (TOTP) for the user
func (u *UserInfo) CreateTOTP() error {
	if u.Email == "" {
		return fmt.Errorf("User must have email address to create TOTP")
	}

	totpgen, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: u.Email,
	})
	if err != nil {
		log.Fatalf("TOTP generation failed: %v\n", err)
	}

	return u.config.CreateProp(u.path("totp"), totpgen.String(), nil)
}

// ClearTOTP removes a time-based one time password (TOTP) for the user
func (u *UserInfo) ClearTOTP() error {
	err := u.config.DeleteProp(u.path("totp"))
	if err != nil {
		return errors.Wrap(err, "TOTP property deletion failed")
	}
	return nil
}
