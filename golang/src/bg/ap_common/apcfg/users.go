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
	UUID              uuid.UUID
	Role              string // User role
	DisplayName       string // User's friendly name
	Email             string // User email
	PreferredLanguage string
	TelephoneNumber   string // User telephone number
	TOTP              string // Time-based One Time Password URL
	Password          string // bcrypt Password
	MD4Password       string // MD4 Password for WPA-EAP/MSCHAPv2
	config            *APConfig
	newUser           bool // need to do creation activities
}

// UserMap maps an account's username to its configuration information
type UserMap map[string]*UserInfo

// newUserFromNode creates a UserInfo from config properties
func newUserFromNode(name string, user *PropertyNode) (*UserInfo, error) {
	uid, err := getStringVal(user, "uid")
	if err != nil {
		// Most likely manual creation of the @/users/[uid] node.
		return nil, errors.Wrap(err, "incomplete user property node")
	}
	if name != uid {
		return nil, fmt.Errorf("prop name '%s' != uid '%s'", name, uid)
	}

	password, _ := getStringVal(user, "userPassword")
	md4password, _ := getStringVal(user, "userMD4Password")
	suuid, _ := getStringVal(user, "uuid")
	xuuid, _ := uuid.FromString(suuid)
	email, _ := getStringVal(user, "email")
	telephoneNumber, _ := getStringVal(user, "telephoneNumber")
	preferredLanguage, _ := getStringVal(user, "preferredLanguage")
	displayName, _ := getStringVal(user, "displayName")
	totp, _ := getStringVal(user, "totp")

	u := &UserInfo{
		UID:               uid,
		UUID:              xuuid,
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

// NewUserInfo is intended for use when creating new users in the config
// store.  It makes a UserInfo, a new UUID, and marks the UserInfo as
// "new", which triggers special behavior in Userinfo.Update.
func (c *APConfig) NewUserInfo(uid string) (*UserInfo, error) {
	if uid == "" {
		return nil, fmt.Errorf("bad uid")
	}
	_, err := c.GetProps("@/users/" + uid)
	if err == nil {
		return nil, fmt.Errorf("user %s already exists", uid)
	}
	if err != ErrNoProp {
		return nil, errors.Wrapf(err, "failed checking if user %s exists", uid)
	}
	return &UserInfo{
		UID:     uid,
		UUID:    uuid.NewV4(),
		config:  c,
		newUser: true,
	}, nil
}

// NoSuchUserError indicates that the named user does not exist in the database
type NoSuchUserError struct {
	uuid string
	uid  string
}

func (e NoSuchUserError) Error() string {
	if e.uuid != "" {
		return fmt.Sprintf("No such user uuid %s", e.uuid)
	} else if e.uid != "" {
		return fmt.Sprintf("No such user uid %s", e.uid)
	}
	// Shouldn't happen
	panic("invalid error")
}

// GetUser fetches the UserInfo structure for a given user
func (c *APConfig) GetUser(uid string) (*UserInfo, error) {
	if uid == "" {
		return nil, fmt.Errorf("uid must be specified")
	}
	user, err := c.GetProps("@/users/" + uid)
	if err != nil {
		if err == ErrNoProp {
			return nil, NoSuchUserError{uid: uid}
		}
		return nil, errors.Wrapf(err, "Failed to get user %s", uid)
	}
	ui, err := newUserFromNode(uid, user)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to make UserInfo")
	}
	ui.config = c
	return ui, nil
}

// GetUserByUUID fetches the UserInfo structure for a given UUID
func (c *APConfig) GetUserByUUID(ruuid uuid.UUID) (*UserInfo, error) {
	users, err := c.GetProps("@/users/")
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to get user list")
	}
	for name, user := range users.Children {
		upn, ok := user.Children["uuid"]
		if !ok {
			continue
		}
		pnuuid, err := uuid.FromString(upn.Value)
		if err != nil {
			continue
		}
		if uuid.Equal(pnuuid, ruuid) {
			ui, err := newUserFromNode(name, user)
			if err != nil {
				return nil, errors.Wrap(err, "Failed to make UserInfo")
			}
			ui.config = c
			return ui, nil
		}
	}
	return nil, NoSuchUserError{uuid: ruuid.String()}
}

// GetUsers fetches the Users subtree, in the form of a map of UID to UserInfo
// structures.
func (c *APConfig) GetUsers() UserMap {
	props, err := c.GetProps("@/users")
	if err != nil {
		return nil
	}

	set := make(map[string]*UserInfo)
	for name, user := range props.Children {
		if us, err := newUserFromNode(name, user); err == nil {
			us.config = c
			set[name] = us
		} else {
			// XXX kludge
			log.Printf("couldn't userinfo %v: %v\n", name, err)
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

// Update saves a modified userinfo to the config store.
// If the user if newUser == true then it does the appropriate
// creation operations.
func (u *UserInfo) Update() error {
	var err error
	var ops []PropertyOp

	if u.UID == "" {
		return fmt.Errorf("user name (uid) must be supplied")
	}
	if u.UID == "" {
		return fmt.Errorf("UUID must be supplied")
	}
	if u.Email == "" {
		return fmt.Errorf("email must be supplied")
	}
	if u.TelephoneNumber == "" {
		return fmt.Errorf("telephone number must be supplied")
	}

	if _, err = mail.ParseAddress(u.Email); err != nil {
		return errors.Wrap(err, "email must be legitimate RFC5322 address: %s")
	}
	phoneNum, err := libphonenumber.Parse(u.TelephoneNumber, "US")
	if err != nil {
		return errors.Wrap(err, "invalid phoneNumber")
	}
	phoneStr := libphonenumber.Format(phoneNum, libphonenumber.INTERNATIONAL)

	// Convenience function to add a value, if non-null to the PropertyOp slice
	addProp := func(p, v string) {
		if v == "" {
			return
		}
		p = u.path(p)
		ops = append(ops, PropertyOp{Op: PropCreate, Name: p, Value: v})
	}
	if u.newUser {
		addProp("uid", u.UID)
		addProp("uuid", u.UUID.String())
	}
	addProp("email", u.Email)
	addProp("displayName", u.DisplayName)
	addProp("telephoneNumber", phoneStr)
	addProp("preferredLanguage", u.PreferredLanguage)
	addProp("role", u.Role)
	_, err = u.config.Execute(ops)
	if err != nil {
		return errors.Wrap(err, "failed to update user")
	}
	return nil
}

// Delete removes a user record from the config store
func (u *UserInfo) Delete() error {
	err := u.config.DeleteProp(u.path(""))
	if err != nil {
		return errors.Wrapf(err, "could not delete user '%s'", u.UID)
	}
	return nil
}

// SetPassword assigns all appropriate password hash properties for the given user.
func (u *UserInfo) SetPassword(passwd string) error {
	// Generate bcrypt password property.
	hps, err := bcrypt.GenerateFromPassword([]byte(passwd), bcrypt.DefaultCost)
	if err != nil {
		return errors.Wrapf(err, "could not encrypt password for %s", u.UID)
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

	// Write out properties
	ops := []PropertyOp{
		{
			Op:    PropCreate,
			Name:  u.path("userPassword"),
			Value: string(hps),
		},
		{
			Op:    PropCreate,
			Name:  u.path("userMD4Password"),
			Value: string(md4s),
		},
	}
	_, err = u.config.Execute(ops)
	if err != nil {
		return errors.Wrapf(err,
			"could not create password properties for %s", u.UID)
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
