/*
 * COPYRIGHT 2019 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package ssh

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"sync"

	"golang.org/x/crypto/ssh"
)

const (
	rsaKeySize          = 2048
	precomputedPoolSize = 2
)

type keypair struct {
	public  []byte
	private []byte
}

var (
	precomputedKeys struct {
		keys []*keypair
		sync.Mutex
	}
)

// generate a new RSA keypair and add it to the list of precomputed pairs
func addKeypair() error {
	key, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return fmt.Errorf("unable to generate key: %v", err)
	}

	// convert both private and public keys into PEM-formatted blocks
	privpem := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}

	pubpem := &pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&key.PublicKey),
	}

	pair := keypair{
		private: pem.EncodeToMemory(privpem),
		public:  pem.EncodeToMemory(pubpem),
	}
	precomputedKeys.Lock()
	precomputedKeys.keys = append(precomputedKeys.keys, &pair)
	precomputedKeys.Unlock()
	return nil
}

// Fetch a precomputed keypair.  Return both keys in PEM-encoded blocks.
func generateRSAKeypair() ([]byte, []byte, error) {
	var k *keypair

	for k == nil {
		if len(precomputedKeys.keys) == 0 {
			// if the queue runs dry, we need to create one
			// on-demand
			if err := addKeypair(); err != nil {
				return nil, nil, err
			}
		}

		precomputedKeys.Lock()
		if len(precomputedKeys.keys) > 0 {
			k = precomputedKeys.keys[0]
			precomputedKeys.keys = precomputedKeys.keys[1:]
		}
		precomputedKeys.Unlock()
	}

	// Start a new keypair creation to replace the one we just used.
	go addKeypair()

	return k.private, k.public, nil
}

// extract a key from a PEM block
func keyFromPEM(key []byte, expected string) ([]byte, error) {
	var data []byte
	var err error

	block, _ := pem.Decode(key)
	if block == nil {
		err = fmt.Errorf("decoding %s PEM block", expected)
	} else if block.Type != expected {
		err = fmt.Errorf("expected %s, found: %s", expected, block.Type)
	} else {
		data = block.Bytes
	}

	return data, err
}

// WritePrivateKey verifies that the provided string contains a valid RSA key in
// PEM format before storing it in a file.
func WritePrivateKey(key []byte, file string) error {
	data, err := keyFromPEM(key, "RSA PRIVATE KEY")
	if err == nil {
		// Attempt to interpret it as an RSA key.
		if _, err = x509.ParsePKCS1PrivateKey(data); err != nil {
			err = fmt.Errorf("parsing private key as RSA: %v", err)
		} else {
			err = ioutil.WriteFile(file, key, 0600)
		}
	}

	return err
}

// WriteAuthorizedKey verifies that the provided string contains a valid RSA key
// in PEM format.  The key is then converted into the ssh 'authorized_keys'
// format and stored in a file.
func WriteAuthorizedKey(key []byte, file string) error {
	sshKey, err := ParsePublicKey(key)
	if err == nil {
		authKey := ssh.MarshalAuthorizedKey(sshKey)

		if err = ioutil.WriteFile(file, authKey, 0644); err != nil {
			err = fmt.Errorf("unable to store public key: %v", err)
		}
	}

	return err
}

// ParsePublicKey takes an RSA key encoded in a PEM block and converts it into
// an ssh.PublicKey.
func ParsePublicKey(pubkey []byte) (ssh.PublicKey, error) {
	data, err := keyFromPEM(pubkey, "RSA PUBLIC KEY")
	if err != nil {
		return nil, err
	}

	// Attempt to interpret it as an RSA key.
	rsa, err := x509.ParsePKCS1PublicKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing public key as RSA: %v", err)
	}

	// Import the key into the ssh framework
	sshKey, err := ssh.NewPublicKey(rsa)
	if err != nil {
		err = fmt.Errorf("converting RSA to ssh: %v", err)
	}
	return sshKey, err
}

// GenerateSSHKeypair generates an ssh keypair.  Both keys are returned and the
// private key is written to a file.
func GenerateSSHKeypair(file string) (string, string, error) {
	var privateRval, publicRval string

	private, public, err := generateRSAKeypair()
	if err != nil {
		err = fmt.Errorf("unable to generate keypair: %v", err)

	} else if err = WritePrivateKey(private, file); err != nil {
		err = fmt.Errorf("unable to write private key %s: %v", file, err)

	} else {
		publicRval = string(public)
		privateRval = string(private)
	}

	return privateRval, publicRval, err
}

// It can take multiple seconds to generate an ssh keypair.  Start creating a
// few now, so they're ready if we need them.
func init() {
	for i := 0; i < precomputedPoolSize; i++ {
		go addKeypair()
	}
}
