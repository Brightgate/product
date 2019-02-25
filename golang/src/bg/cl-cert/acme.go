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
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xenolf/lego/certcrypto"
	"github.com/xenolf/lego/lego"
	"github.com/xenolf/lego/registration"
)

type acmeUser struct {
	email        string
	registration *registration.Resource
	key          crypto.PrivateKey
	keyType      certcrypto.KeyType
}

// GetEmail implements lego/acme.User
func (u acmeUser) GetEmail() string {
	return u.email
}

// GetRegistration implements lego/acme.User
func (u acmeUser) GetRegistration() *registration.Resource {
	return u.registration
}

// GetPrivateKey implements lego/acme.User
func (u acmeUser) GetPrivateKey() crypto.PrivateKey {
	return u.key
}

// MarshalJSON implements json/Marshaler
func (u acmeUser) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	buf.WriteString(`"email":`)
	j, _ := json.Marshal(u.email)
	buf.Write(j)
	buf.WriteString(`,"registration":`)
	j, _ = json.Marshal(u.registration)
	buf.Write(j)

	buf.WriteString(`,"key":`)
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(u.key)
	if err != nil {
		return nil, err
	}
	var pemType string
	switch u.key.(type) {
	case *ecdsa.PrivateKey:
		pemType = "EC"
	case *rsa.PrivateKey:
		pemType = "RSA"
	default:
		panic("unknown key type")
	}
	block := &pem.Block{
		Type:  pemType + " PRIVATE KEY",
		Bytes: pkcs8Bytes,
	}
	pkcs8PEMBytes := pem.EncodeToMemory(block)
	j, _ = json.Marshal(string(pkcs8PEMBytes))
	buf.Write(j)

	buf.WriteByte('}')

	return buf.Bytes(), nil
}

// acmeUserWrapper is a shim struct to help us decode a JSON-encoded acmeUser.
// In particular, the encoding of the `key` attribute is special, and can't be
// done in the normal fashion, so initially we decode it as a string, and then
// convert that specially.
type acmeUserWrapper struct {
	Email        string                 `json:"email"`
	Registration *registration.Resource `json:"registration"`
	Key          string                 `json:"key"`
}

// UnmarshalJSON implements json/Unmarshaler
func (u *acmeUser) UnmarshalJSON(b []byte) error {
	var stuff acmeUserWrapper
	err := json.Unmarshal(b, &stuff)
	if err != nil {
		return err
	}
	u.email = stuff.Email
	u.registration = stuff.Registration
	block, _ := pem.Decode([]byte(stuff.Key))
	if block == nil {
		return fmt.Errorf("Key not PEM encoded")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	u.key = key
	switch u.key.(type) {
	case *ecdsa.PrivateKey:
		curveName := u.key.(*ecdsa.PrivateKey).Curve.Params().Name
		switch curveName {
		case "P-256":
			u.keyType = certcrypto.EC256
		case "P-384":
			u.keyType = certcrypto.EC384
		default:
			return fmt.Errorf("Unknown elliptic curve %q", curveName)
		}
	case *rsa.PrivateKey:
		bits := u.key.(*rsa.PrivateKey).N.BitLen()
		switch bits {
		case 2048:
			u.keyType = certcrypto.RSA2048
		case 4096:
			u.keyType = certcrypto.RSA4096
		case 8192:
			u.keyType = certcrypto.RSA8192
		}
	default:
		panic("Shouldn't ever get here")
	}
	return nil
}

func acmeRegister(cmd *cobra.Command, args []string) error {
	email, _ := cmd.Flags().GetString("email")
	url, _ := cmd.Flags().GetString("url")
	keyTypeStr, _ := cmd.Flags().GetString("key-type")

	keyType := map[string]certcrypto.KeyType{
		"rsa2048": certcrypto.RSA2048,
		"rsa4096": certcrypto.RSA4096,
		"rsa8192": certcrypto.RSA8192,
		"ec256":   certcrypto.EC256,
		"ec384":   certcrypto.EC384,
	}[strings.ToLower(keyTypeStr)]
	if keyType == "" {
		return requiredUsage{
			cmd: cmd,
			msg: fmt.Sprintf("Invalid key type %q", keyTypeStr),
			explanation: "The valid key types are 'rsa2048', " +
				"'rsa4096', 'rsa8192', 'ec256', and 'ec384'",
		}
	}
	var privateKey crypto.PrivateKey
	var err error
	switch keyType {
	case certcrypto.RSA2048:
		privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	case certcrypto.RSA4096:
		privateKey, err = rsa.GenerateKey(rand.Reader, 4096)
	case certcrypto.RSA8192:
		privateKey, err = rsa.GenerateKey(rand.Reader, 8192)
	case certcrypto.EC256:
		privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case certcrypto.EC384:
		privateKey, err = ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	}
	if err != nil {
		return err
	}
	leUser := acmeUser{
		email:   email,
		key:     privateKey,
		keyType: keyType,
	}
	leConfig := lego.NewConfig(&leUser)
	leConfig.CADirURL = url
	leConfig.Certificate.KeyType = leUser.keyType
	client, err := lego.NewClient(leConfig)
	if err != nil {
		return err
	}
	reg, err := client.Registration.Register(
		registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return err
	}
	leUser.registration = reg
	j, err := json.Marshal(leUser)
	if err != nil {
		return err
	}
	fmt.Println(string(j))
	return nil
}

func acmeSetup(config, url string) (*lego.Config, *lego.Client, error) {
	bytes, err := ioutil.ReadFile(config)
	if err != nil {
		return nil, nil, err
	}
	var leUser acmeUser
	if err := json.Unmarshal(bytes, &leUser); err != nil {
		return nil, nil, err
	}
	leConfig := lego.NewConfig(&leUser)
	leConfig.CADirURL = url
	leConfig.Certificate.KeyType = leUser.keyType
	client, err := lego.NewClient(leConfig)
	return leConfig, client, err
}
