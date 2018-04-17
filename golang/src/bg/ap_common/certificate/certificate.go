//
// COPYRIGHT 2018 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package certificate

import (
	"crypto/rsa"
	// "crypto/ecdsa"
	// "crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"os"
	"time"

	"bg/ap_common/aputil"
	"bg/ap_common/broker"
	"bg/base_def"
	"bg/base_msg"

	"github.com/pkg/errors"

	"github.com/golang/protobuf/proto"
)

const (
	applianceTempCryptoDir = "/tmp"
	pathLELiveDir          = "/etc/letsencrypt/live"
	durationSSCert         = 7 * 24 * 60 * 60 * 1e9
	organization           = "Brightgate Inc."
	rsaKeySize             = 2048
)

// Routines in this file are used to test and generate certificates when
// the Let's Encrypt key and certificate set are absent, expired, or
// incomplete.

// Self-signed certificates should be created in a non-persistent filesystem.

// Functions that return a set of pathnames will always respond with
// private key, certificate, CA certificate chain, combined, and error.

// If we detect an invalid certificate, we send a sys.error message to the bus,
// under TOPIC_ERROR.  If we have to generate a self-signed certificate, we
// also send a sys.error message to the bus, under TOPIC_ERROR.

func fileExists(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return errors.Wrapf(err, "'%s' does not exist", path)
	}
	return nil
}

func willCertificateExpire(cert *x509.Certificate, at time.Time) bool {
	if at.Before(cert.NotBefore) {
		return true
	}
	if at.After(cert.NotAfter) {
		return true
	}
	return false
}

// For Go, we must use the full certificate chain.
func getLEKeyCertPaths(hostname string, at time.Time) (keyfn string, certfn string, chainfn string, fullchainfn string, err error) {
	keyfn = fmt.Sprintf("/%s/%s/privkey.pem", pathLELiveDir, hostname)
	certfn = fmt.Sprintf("/%s/%s/cert.pem", pathLELiveDir, hostname)
	chainfn = fmt.Sprintf("/%s/%s/chain.pem", pathLELiveDir, hostname)
	fullchainfn = fmt.Sprintf("/%s/%s/fullchain.pem", pathLELiveDir, hostname)

	fcerr := fileExists(fullchainfn)
	kerr := fileExists(keyfn)

	if kerr != nil {
		err = errors.Wrap(kerr, "key file missing")
		return
	}

	if fcerr != nil {
		err = errors.Wrap(fcerr, "certificate file missing")
		return
	}

	// certificate must be valid
	// read certificate file into buffer
	certb, err := ioutil.ReadFile(fullchainfn)
	if err != nil {
		// failed read
		err = fmt.Errorf("could not read certificate file '%s': %s",
			fullchainfn, err)
		return
	}

	certd, _ := pem.Decode(certb)

	// parse certificate from buffer
	cert, err := x509.ParseCertificate(certd.Bytes)
	if err != nil {
		// failed parse
		err = fmt.Errorf("could not parse certificate: %s", err)
		return
	}

	// check if valid
	fmt.Printf("cert: subject = %v issuer = %v notbefore = %v notafter = %v\n",
		cert.Subject, cert.Issuer, cert.NotBefore, cert.NotAfter)
	if willCertificateExpire(cert, at) {
		// XXX how expired?
		err = fmt.Errorf("certificate expired")
		return
	}

	return
}

func sendCertRenewEvent(brokerd *broker.Broker, forceRefresh bool) {
	reason := base_msg.EventSysError_RENEWED_SSL_CERTIFICATE
	message := "self-signed certificate needed"

	errormsg := &base_msg.EventSysError{
		Timestamp: aputil.NowToProtobuf(),
		Sender:    proto.String(brokerd.Name),
		Reason:    &reason,
		Message:   &message,
	}

	if forceRefresh {
		log.Printf("sending sys.error <- %v", errormsg)
	}

	err := brokerd.Publish(errormsg, base_def.TOPIC_ERROR)
	if err != nil {
		log.Printf("couldn't publish sys.error %v: %v", errormsg, err)
	}
}

func createSSKeyCert(brokerd *broker.Broker, cryptodir string, hostname string, at time.Time, forceRefresh bool) (keyfn string, certfn string, chainfn string, fullchainfn string, err error) {
	var (
		priv      *rsa.PrivateKey
		serialMax big.Int
	)
	serialMax.SetInt64(math.MaxInt64)

	needKey := false
	needCert := false

	keyfn = fmt.Sprintf("%s/privkey.pem", cryptodir)
	certfn = fmt.Sprintf("%s/cert.pem", cryptodir)
	chainfn = fmt.Sprintf("%s/cert.pem", cryptodir)
	fullchainfn = fmt.Sprintf("%s/cert.pem", cryptodir)

	// Do we have a key?  If so, reuse.
	keyb, err := ioutil.ReadFile(keyfn)
	if err == nil {
		keyd, _ := pem.Decode(keyb)
		if keyd.Type == "RSA PRIVATE KEY" {
			priv, err = x509.ParsePKCS1PrivateKey(keyd.Bytes)
		} else {
			needKey = true
		}
	} else {
		// which error?
		needKey = true
	}

	if forceRefresh {
		log.Printf("forced key/certificate refresh")
		needKey = true
	}

	if !needKey {
		// it is possible that the certificate has expired
		// certificate must be valid
		// read certificate file into buffer
		certb, err := ioutil.ReadFile(certfn)
		if err != nil {
			// failed read
			err = fmt.Errorf("could not read certificate file: %s", err)
			needCert = true
		}

		certd, _ := pem.Decode(certb)

		// parse certificate from buffer
		cert, err := x509.ParseCertificate(certd.Bytes)
		if err != nil {
			// failed parse
			err = fmt.Errorf("could not parse certificate: %s", err)
			needCert = true
		}

		// check if valid
		if willCertificateExpire(cert, at) {
			// XXX how expired?
			err = fmt.Errorf("certificate expired")
			needCert = true
		}
	} else {
		log.Printf("generating private key")
		priv, err = rsa.GenerateKey(rand.Reader, rsaKeySize)
		needCert = true
	}

	if !needCert {
		err = nil
		return
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(durationSSCert)
	serialNumber, err := rand.Int(rand.Reader, &serialMax)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{organization},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		IsCA:                  true,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	template.DNSNames = append(template.DNSNames, hostname)

	log.Printf("generating self-signed certificate")
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		err = fmt.Errorf("Failed to create certificate: %s", err)
		return
	}

	keyf, err := os.OpenFile(keyfn, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		err = fmt.Errorf("failed to open %s for writing: %s", keyfn, err)
		return
	}

	kb := x509.MarshalPKCS1PrivateKey(priv)

	pem.Encode(keyf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: kb})

	keyf.Close()

	certf, err := os.Create(certfn)
	if err != nil {
		err = fmt.Errorf("failed to open %s for writing: %s", certfn, err)
		return
	}
	pem.Encode(certf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certf.Close()

	if brokerd != nil {
		sendCertRenewEvent(brokerd, forceRefresh)
	}

	return
}

// GetKeyCertPaths will attempt to validate the preferred SSL
// certificate and private key on the appliance.  If valid, the full
// pathnames for each of these files will be returned.  If absent or
// invalid, GetKeyCertPaths will generate a self-signed certificate and
// new private key and return those pathnames.  If forceRefresh is true, then
// the Let's Encrypt certificate is ignored and a new self-signed certificate
// is unconditionally generated.
func GetKeyCertPaths(brokerd *broker.Broker, hostname string, at time.Time,
	forceRefresh bool) (string, string, string, string, error) {
	keyfn, certfn, chainfn, fullchainfn, err := getLEKeyCertPaths(hostname, at)
	if err == nil && !forceRefresh {
		return keyfn, certfn, chainfn, fullchainfn, err
	}

	log.Printf("LE certs not available: %v\n", err)

	return createSSKeyCert(brokerd, applianceTempCryptoDir, hostname, at,
		forceRefresh)
}
