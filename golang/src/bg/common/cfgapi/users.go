/*
 * COPYRIGHT 2020 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgapi

import (
	"context"
	"fmt"
	"log"
	"net/mail"

	"bg/common/wgconf"

	"github.com/pkg/errors"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/md4"

	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/satori/uuid"
	"github.com/ttacon/libphonenumber"
)

// UserInfo contains all of the configuration information for an appliance user
// account.  Expected roles are: "SITE_ADMIN", "SITE_USER",
// "SITE_GUEST", "CUST_ADMIN", "CUST_USER", "CUST_GUEST".
type UserInfo struct {
	UID string // Username
	// If SelfProvisioning is true, UUID should match cloud account UUID
	UUID            uuid.UUID
	Role            string // User role
	DisplayName     string // User's friendly name
	Email           string // User email
	TelephoneNumber string // User telephone number
	Password        string // bcrypt Password
	MD4Password     string // MD4 Password for WPA-EAP/MSCHAPv2
	// User was created by cloud self-provisioning; if true, UUID matches
	// cloud user UUID
	SelfProvisioning bool
	config           *Handle
	newUser          bool // need to do creation activities

	WGConfig []*wgconf.UserConf

	// PropertyOps which accumulate password to set.
	passwordOps []PropertyOp
}

func wgConfig(user, mac string, root *PropertyNode) (*wgconf.UserConf, error) {
	var key, addr string
	var err error

	c := &wgconf.UserConf{
		Mac:  mac,
		User: user,
	}
	c.ID, _ = root.GetChildInt("id")
	c.Label, _ = root.GetChildString("label")

	if key, _ = root.GetChildString("public_key"); key == "" {
		return nil, fmt.Errorf("missing public key")
	} else if err = c.SetKey(key); err != nil {
		return nil, err
	}

	if addr, _ = root.GetChildString("assigned_ip"); addr == "" {
		return nil, fmt.Errorf("missing ip address")
	} else if err = c.SetIPAddress(addr); err != nil {
		return nil, err
	}

	if subnets, _ := root.GetChildString("allowed_ips"); subnets != "" {
		if err = c.SetSubnets(subnets); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func wgConfigs(user *UserInfo, root *PropertyNode) []*wgconf.UserConf {
	var s []*wgconf.UserConf

	if len(root.Children) > 0 {
		s = make([]*wgconf.UserConf, 0)
		for mac, key := range root.Children {
			c, err := wgConfig(user.UID, mac, key)
			if err != nil {
				log.Printf("bad vpn key %s/%s: %v",
					user.UID, mac, err)
			} else {
				s = append(s, c)
			}
		}
	}
	return s
}

// UserMap maps an account's username to its configuration information
type UserMap map[string]*UserInfo

// newUserFromNode creates a UserInfo from config properties
func newUserFromNode(name string, user *PropertyNode) (*UserInfo, error) {
	uid, err := user.GetChildString("uid")
	if err != nil {
		// Most likely manual creation of the @/users/[uid] node.
		return nil, errors.Wrap(err, "incomplete user property node")
	}
	if name != uid {
		return nil, fmt.Errorf("prop name '%s' != uid '%s'", name, uid)
	}

	password, _ := user.GetChildString("user_password")
	md4password, _ := user.GetChildString("user_md4_password")
	suuid, _ := user.GetChildString("uuid")
	xuuid, _ := uuid.FromString(suuid)
	email, _ := user.GetChildString("email")
	telephoneNumber, _ := user.GetChildString("telephone_number")
	displayName, _ := user.GetChildString("display_name")
	selfProvisioning, _ := user.GetChildBool("self_provisioning")

	u := &UserInfo{
		UID:              uid,
		UUID:             xuuid,
		Email:            email,
		TelephoneNumber:  telephoneNumber,
		DisplayName:      displayName,
		Password:         password,
		MD4Password:      md4password,
		SelfProvisioning: selfProvisioning,
	}

	if vpn, ok := user.Children["vpn"]; ok {
		u.WGConfig = wgConfigs(u, vpn)
	}

	return u, nil
}

// NewUserInfo is intended for use when creating new users in the config
// store.  It makes a UserInfo, a new UUID, and marks the UserInfo as
// "new", which triggers special behavior in Userinfo.Update.
func (c *Handle) NewUserInfo(uid string) (*UserInfo, error) {
	if uid == "" {
		return nil, fmt.Errorf("bad uid")
	}
	_, err := c.GetProps("@/users/" + uid)
	if err == nil {
		return nil, fmt.Errorf("user %s already exists", uid)
	}
	if err != ErrNoProp {
		return nil, err
	}
	return &UserInfo{
		UID:     uid,
		UUID:    uuid.NewV4(),
		config:  c,
		newUser: true,
	}, nil
}

// NewSelfProvisionUserInfo is for use in the Cloud->Appliance provisioning
// scenario.  It avoids the upfront test for user existence.
func (c *Handle) NewSelfProvisionUserInfo(uid string, uu uuid.UUID) (*UserInfo, error) {
	if uid == "" {
		return nil, fmt.Errorf("bad uid")
	}
	if uu == uuid.Nil {
		return nil, fmt.Errorf("bad uuid")
	}
	return &UserInfo{
		UID:              uid,
		UUID:             uu,
		SelfProvisioning: true,
		config:           c,
		newUser:          true,
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
func (c *Handle) GetUser(uid string) (*UserInfo, error) {
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
func (c *Handle) GetUserByUUID(ruuid uuid.UUID) (*UserInfo, error) {
	users, err := c.GetProps("@/users/")
	if err != nil {
		if err == ErrNoProp {
			return nil, NoSuchUserError{uuid: ruuid.String()}
		}
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
func (c *Handle) GetUsers() UserMap {
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
//
// If error is indicated, that indicates a precondition failure
// before the command execution.
func (u *UserInfo) Update(ctx context.Context) (CmdHdl, error) {
	var err error
	var ops []PropertyOp

	if u.UID == "" {
		return nil, fmt.Errorf("user name (uid) must be supplied")
	}
	if u.UUID == uuid.Nil {
		return nil, fmt.Errorf("UUID must be supplied")
	}
	if u.Email == "" {
		return nil, fmt.Errorf("email must be supplied")
	}
	if u.TelephoneNumber == "" {
		return nil, fmt.Errorf("telephone number must be supplied")
	}

	if _, err = mail.ParseAddress(u.Email); err != nil {
		return nil, errors.Wrap(err, "email must be legitimate RFC5322 address: %s")
	}
	phoneNum, err := libphonenumber.Parse(u.TelephoneNumber, "US")
	if err != nil {
		return nil, errors.Wrap(err, "invalid phoneNumber")
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

	// This property is now somewhat misnamed.  It really means "cloud
	// managed".
	if u.SelfProvisioning {
		addProp("self_provisioning", "true")
	}

	if u.newUser {
		addProp("uid", u.UID)
		addProp("uuid", u.UUID.String())
	} else {
		// If not new, then make sure we're operating on the UUID we
		// expect.
		ops = append(ops, PropertyOp{
			Op:    PropTestEq,
			Name:  u.path("uuid"),
			Value: u.UUID.String(),
		})
	}
	addProp("email", u.Email)
	addProp("telephone_number", phoneStr)
	addProp("display_name", u.DisplayName)
	addProp("role", u.Role)
	ops = append(ops, u.passwordOps...)
	return u.config.Execute(ctx, ops), nil
}

// Delete sets up the removal of a user, and returns a command
// handle so the caller can wait (or not) for the result.
//
// This routine will fail (ErrNoProp) if the user does not exist.
func (u *UserInfo) Delete(ctx context.Context) CmdHdl {
	ops := []PropertyOp{
		{
			Op:   PropDelete,
			Name: u.path(""),
		},
	}
	return u.config.Execute(ctx, ops)
}

// HashUserPassword generates a bcrypted password suitable for use in the
// userPassword property.
func HashUserPassword(passwd string) (string, error) {
	// Generate bcrypt password property.
	hps, err := bcrypt.GenerateFromPassword([]byte(passwd), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hps), nil
}

// HashMSCHAPv2Password generates the MD4-hashed MSCHAP-v2 password.  Note that
// the strength of this hashing is very low.
func HashMSCHAPv2Password(passwd string) (string, error) {
	enc := unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM).NewEncoder()
	md4ps := md4.New()
	t := transform.NewWriter(md4ps, enc)
	_, err := t.Write([]byte(passwd))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", md4ps.Sum(nil)), nil
}

// SetPasswordsByHash generates PropOps for setting passwords given the
// tuple of password hash values (generated by HashUserPassword() and
// HashMSCHAPv2Password())
func (u *UserInfo) SetPasswordsByHash(userPassHash, mschapv2Hash string) {
	u.passwordOps = []PropertyOp{
		{
			Op:    PropCreate,
			Name:  u.path("user_password"),
			Value: userPassHash,
		},
		{
			Op:    PropCreate,
			Name:  u.path("user_md4_password"),
			Value: mschapv2Hash,
		},
	}
}

// SetPassword assigns all appropriate password hash properties for the given user.
func (u *UserInfo) SetPassword(passwd string) error {
	// Generate bcrypt password property.
	user, err := HashUserPassword(passwd)
	if err != nil {
		return errors.Wrapf(err, "could not encrypt password for %s", u.UID)
	}

	mschapv2, err := HashMSCHAPv2Password(passwd)
	if err != nil {
		return errors.Wrapf(err, "could not generate MSCHAP-v2 password for %s", u.UID)
	}
	u.SetPasswordsByHash(user, mschapv2)
	return nil
}

// SetNoPassword sends config commands to remove password hash properties
// for the given user.
func (u *UserInfo) SetNoPassword() {
	u.passwordOps = []PropertyOp{
		{
			Op:    PropCreate,
			Name:  u.path("user_password"),
			Value: "XXX",
		},
		{
			Op:    PropCreate,
			Name:  u.path("user_md4_password"),
			Value: "XXX",
		},
		{
			Op:   PropDelete,
			Name: u.path("user_password"),
		},
		{
			Op:   PropDelete,
			Name: u.path("user_md4_password"),
		},
	}
}
