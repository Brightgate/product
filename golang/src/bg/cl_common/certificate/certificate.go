/*
 * COPYRIGHT 2019 Brightgate Inc. All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package certificate

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math"
	"math/big"
	"time"
)

const (
	forever      = 20 * 365 * 24 * time.Hour
	organization = "Brightgate Inc."
	rsaKeySize   = 2048
)

// CreateSSKeyCert creates a self-signed key/cert pair for the given domain and
// returns the PEM-encoded byte slices.
func CreateSSKeyCert(domainname string) ([]byte, []byte, error) {
	var serialMax big.Int
	serialMax.SetInt64(math.MaxInt64)

	log.Printf("generating private key")
	priv, err := rsa.GenerateKey(rand.Reader, rsaKeySize)
	if err != nil {
		return nil, nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(forever)
	serialNumber, err := rand.Int(rand.Reader, &serialMax)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   domainname,
			Organization: []string{organization},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		IsCA:                  true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if domainname != "" {
		template.DNSNames = append(template.DNSNames, domainname, "*."+domainname)
	}

	log.Printf("generating self-signed certificate")
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		err = fmt.Errorf("Failed to create certificate: %s", err)
		return nil, nil, err
	}
	kb := x509.MarshalPKCS1PrivateKey(priv)

	keyf := new(bytes.Buffer)
	if err = pem.Encode(keyf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: kb}); err != nil {
		return nil, nil, err
	}
	certf := new(bytes.Buffer)
	if err = pem.Encode(certf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		return nil, nil, err
	}

	return keyf.Bytes(), certf.Bytes(), err
}
