//
// COPYRIGHT 2019 Brightgate Inc. All rights reserved.
//
// This copyright notice is Copyright Management Information under 17 USC 1202
// and is included to protect this work and deter copyright infringement.
// Removal or alteration of this Copyright Management Information without the
// express written permission of Brightgate Inc is prohibited, and any
// such unauthorized removal or alteration will be a violation of federal law.
//

package certificate

import (
	"context"
	"crypto/rsa"
	// "crypto/ecdsa"
	// "crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"bg/ap_common/platform"
	"bg/common/cfgapi"

	"github.com/pkg/errors"
)

const (
	applianceTempCryptoDir = "/tmp"
	pathLELiveDir          = "/etc/letsencrypt/live"
	pathSSLDir             = "__APSECRET__/ssl/cur"
	durationSSCert         = 7 * 24 * 60 * 60 * 1e9
	organization           = "Brightgate Inc."
	rsaKeySize             = 2048
)

var (
	plat *platform.Platform
)

// Routines in this file are used to manage the certificate state on the
// appliance local storage.  This includes generating certificates when the
// Let's Encrypt key and certificate set are absent, expired, or incomplete, as
// well as installing certificates we get from the cloud, and notifying
// interested daemons via the config tree that they're available.

// Self-signed certificates should be created in a non-persistent filesystem.

// Functions that return a set of pathnames will always respond with
// private key, certificate, CA certificate chain, combined, and error.

// CertPaths represents the four possible pathnames to the key material.
type CertPaths struct {
	// Key is the path to the private key material.
	Key string
	// Cert is the path to the certificate.
	Cert string
	// Chain is the path to the issuer (intermediate) certificate.
	Chain string
	// FullChain is the path to the combined cert and issuer cert.
	FullChain string
}

func fileExists(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return errors.Wrapf(err, "'%s' does not exist", path)
	}
	return nil
}

// How far outside the validity window of the certificate is the time `at`?  If
// the returned duration is positive, the time is past the expiration; if it is
// negative, the time is before the start of the window; if it is zero, the
// certificate will be valid at that time.
func timeOutsideValidity(cert *x509.Certificate, at time.Time) time.Duration {
	if at.Before(cert.NotBefore) {
		return at.Sub(cert.NotBefore) // negative duration
	}
	if at.After(cert.NotAfter) {
		return at.Sub(cert.NotAfter) // positive duration
	}
	return time.Duration(0)
}

// Return the paths to the files containing the private key, certificate, issuer
// certificate, and combined certificates, provided to us by the cloud
// infrastructure.  If arguments are passed, use them as the base directory
// instead.  These live inside $APROOT, so we make sure to expand the paths.
func getCloudKeyCertPaths(plat *platform.Platform, path ...string) *CertPaths {
	if len(path) == 0 {
		path = append(path, pathSSLDir)
	}
	pathfn := func(fn string) string {
		p := append(path, fn)
		p = append([]string{"__APROOT__"}, p...)
		return plat.ExpandDirPath(p...)
	}
	return &CertPaths{
		Key:       pathfn("privkey.pem"),
		Cert:      pathfn("cert.pem"),
		Chain:     pathfn("chain.pem"),
		FullChain: pathfn("fullchain.pem"),
	}
}

// Return the paths to the files containing the private key, certificate, issuer
// certificate, and combined certificates, from a Let's Encrypt certbot
// installation.
func getLEKeyCertPaths(hostname string) *CertPaths {
	pathfn := func(fn string) string {
		return filepath.Join(pathLELiveDir, hostname, fn)
	}
	return &CertPaths{
		Key:       pathfn("privkey.pem"),
		Cert:      pathfn("cert.pem"),
		Chain:     pathfn("chain.pem"),
		FullChain: pathfn("fullchain.pem"),
	}
}

// Validate the certificate: the key and combined certificate files must exist,
// and the certificate must expire after, but not before, `at`.
func validateCertificate(paths *CertPaths, at time.Time) (*x509.Certificate, error) {
	fcerr := fileExists(paths.FullChain)
	kerr := fileExists(paths.Key)

	if kerr != nil {
		return nil, errors.Wrap(kerr, "key file missing")
	}

	if fcerr != nil {
		return nil, errors.Wrap(fcerr, "certificate file missing")
	}

	// certificate must be valid
	// read certificate file into buffer
	certb, err := ioutil.ReadFile(paths.FullChain)
	if err != nil {
		// failed read
		err = errors.Wrapf(err, "could not read certificate file '%s'",
			paths.FullChain)
		return nil, err
	}

	certd, _ := pem.Decode(certb)

	// parse certificate from buffer
	cert, err := x509.ParseCertificate(certd.Bytes)
	if err != nil {
		// failed parse
		err = errors.Wrapf(err, "could not parse certificate")
		return nil, err
	}

	fp := sha1.Sum(cert.Raw)
	fpstr := hex.EncodeToString(fp[:])

	// check if valid
	log.Printf("cert: subject = %v issuer = %v "+
		"notbefore = %v notafter = %v fingerprint = %v\n",
		cert.Subject, cert.Issuer, cert.NotBefore, cert.NotAfter, fpstr)
	if expiredBy := timeOutsideValidity(cert, at); expiredBy != time.Duration(0) {
		if expiredBy < time.Duration(0) {
			err = fmt.Errorf("certificate isn't valid for %s", -expiredBy)
		} else {
			err = fmt.Errorf("certificate expired %s ago", expiredBy)
		}
		return nil, err
	}

	return cert, nil
}

func sendDomainChangeEvent(config *cfgapi.Handle, domain string) error {
	origDomain, err := config.GetDomain()
	if err != nil {
		return err
	}
	if origDomain == domain {
		return nil
	}

	return config.SetProp("@/siteid", domain, nil)
}

func sendCertRenewEvent(config *cfgapi.Handle, fp string, cert *x509.Certificate, origin string) error {
	var ops []cfgapi.PropertyOp

	if origDomain, err := config.GetDomain(); err == nil {
		domain := cert.Subject.CommonName
		if origDomain != domain {
			ops = append(ops, cfgapi.PropertyOp{
				Op:    cfgapi.PropSet,
				Name:  "@/siteid",
				Value: domain,
			})
		}
	}

	// Move the state of the cert in the config tree from "available" to
	// "installed".  This is a signal to processes who care about the
	// certificate change to reconfigure (or restart) themselves.
	prop := fmt.Sprintf("@/certs/%s/state", fp)
	propNode, err := config.GetProps(prop)
	var expires *time.Time
	if err == cfgapi.ErrNoProp {
		// If we're downloading without being told there's a cert, the
		// property probably won't exist.
		expires = &cert.NotAfter
		ops = append(ops, cfgapi.PropertyOp{
			Op:      cfgapi.PropCreate,
			Name:    prop,
			Value:   "installed",
			Expires: expires,
		})
	} else if err != nil {
		// We could parse the certificate and grab the expiration date
		// if this happens, but the likelihood is that the SetProp will
		// fail, too.
		return err
	} else {
		if propNode.Value != "available" {
			log.Printf("%s transitioning to 'installed' from unknown state '%s'",
				prop, propNode.Value)
		}
		expires = propNode.Expires
		ops = append(ops, cfgapi.PropertyOp{
			Op:      cfgapi.PropSet,
			Name:    prop,
			Value:   "installed",
			Expires: expires,
		})
	}

	// Record where the certificate came from
	ops = append(ops, cfgapi.PropertyOp{
		Op:      cfgapi.PropCreate,
		Name:    fmt.Sprintf("@/certs/%s/origin", fp),
		Value:   origin,
		Expires: expires,
	})

	_, err = config.Execute(context.TODO(), ops).Wait(context.TODO())
	if err != nil {
		return err
	}

	// Change the previous cert's status from "installed" to "replaced", so
	// we (humans) don't get confused in the period between the renewal and
	// the expiration.  This isn't hugely important, so ignore errors.
	certNode, err := config.GetProps("@/certs")
	if err != nil {
		return nil
	}

	for ofp, node := range certNode.Children {
		stateNode := node.Children["state"]
		if stateNode != nil && stateNode.Value == "installed" && ofp != fp {
			nodestr := fmt.Sprintf("@/certs/%s/state", ofp)
			config.SetProp(nodestr, "replaced", stateNode.Expires)
		}
	}

	return nil
}

func createSSKeyCert(config *cfgapi.Handle, cryptodir string, domainname string, at time.Time, forceRefresh bool) (*CertPaths, error) {
	var (
		priv      *rsa.PrivateKey
		serialMax big.Int
	)
	serialMax.SetInt64(math.MaxInt64)

	needKey := false
	needCert := false

	pathfn := func(fn string) string {
		return filepath.Join(cryptodir, fn)
	}
	paths := &CertPaths{
		Key:       pathfn("privkey.pem"),
		Cert:      pathfn("cert.pem"),
		Chain:     pathfn("cert.pem"),
		FullChain: pathfn("cert.pem"),
	}

	// Do we have a key?  If so, reuse.
	keyb, err := ioutil.ReadFile(paths.Key)
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
		var cert *x509.Certificate
		var certb []byte

		log.Printf("found existing private key")

		// it is possible that the certificate has expired
		// certificate must be valid
		// read certificate file into buffer
		if certb, err = ioutil.ReadFile(paths.Cert); err != nil {
			// failed read
			err = fmt.Errorf("could not read certificate file: %s", err)
			needCert = true
		}

		certd, _ := pem.Decode(certb)

		// parse certificate from buffer
		cert, err = x509.ParseCertificate(certd.Bytes)
		if err != nil {
			// failed parse
			err = fmt.Errorf("could not parse certificate: %s", err)
			needCert = true
		}

		// check if valid
		if expiredBy := timeOutsideValidity(cert, at); expiredBy != time.Duration(0) {
			if expiredBy > time.Duration(0) {
				err = fmt.Errorf("certificate will expire in %s", expiredBy)
			} else {
				err = fmt.Errorf("certificate expired %s ago", -expiredBy)
			}
			needCert = true
		}
	} else {
		log.Printf("generating private key")
		priv, err = rsa.GenerateKey(rand.Reader, rsaKeySize)
		needCert = true
	}

	if !needCert {
		log.Printf("found existing self-signed certificate")
		return paths, nil
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(durationSSCert)
	serialNumber, err := rand.Int(rand.Reader, &serialMax)

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

	template.DNSNames = append(template.DNSNames, domainname, "*."+domainname)

	log.Printf("generating self-signed certificate")
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		err = fmt.Errorf("Failed to create certificate: %s", err)
		return nil, err
	}

	keyf, err := os.OpenFile(paths.Key, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		err = fmt.Errorf("failed to open %s for writing: %s", paths.Key, err)
		return nil, err
	}

	kb := x509.MarshalPKCS1PrivateKey(priv)

	pem.Encode(keyf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: kb})

	keyf.Close()

	certf, err := os.Create(paths.Cert)
	if err != nil {
		err = fmt.Errorf("failed to open %s for writing: %s", paths.Cert, err)
		return nil, err
	}
	pem.Encode(certf, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	certf.Close()

	if config != nil {
		fp := sha1.Sum(derBytes)
		fpstr := hex.EncodeToString(fp[:])
		sendCertRenewEvent(config, fpstr, &template, "self")
	}

	return paths, nil
}

// InstallCert takes a private key, certificate, and issuer certificate, encoded
// as DER, and writes PEM files to the appropriate paths.  It will notify any
// listeners that the certificate is installed.  It does not attempt to protect
// against multiple writers.
func InstallCert(key, cert, issuerCert []byte, fp string, config *cfgapi.Handle) error {
	rawFP := sha1.Sum(cert)
	certFP := hex.EncodeToString(rawFP[:])
	if fp != certFP && fp != "" {
		return fmt.Errorf("Fingerprint of provided certificate (%s) "+
			"doesn't match expected fingerprint (%s)", certFP, fp)
	}
	if certFP == "" {
		return fmt.Errorf("fingerprint of provided certificate is empty")
	}

	// Put the files in a directory named by the fingerprint
	paths := getCloudKeyCertPaths(plat, filepath.Dir(pathSSLDir), certFP)
	tmpDir := filepath.Dir(paths.Key)

	// Just blow away the existing directory, on the offchance it exists.
	if err := os.RemoveAll(tmpDir); err != nil {
		return err
	}
	if err := os.Mkdir(tmpDir, 0755); err != nil {
		return err
	}

	keyf, err := os.OpenFile(paths.Key, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err != pem.Encode(keyf, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: key}) {
		return err
	}
	keyf.Close()

	certf, err := os.Create(paths.Cert)
	if err != nil {
		return err
	}
	if err = pem.Encode(certf, &pem.Block{Type: "CERTIFICATE", Bytes: cert}); err != nil {
		return err
	}
	certf.Close()

	chainf, err := os.Create(paths.Chain)
	if err != nil {
		return err
	}
	if err = pem.Encode(chainf, &pem.Block{Type: "CERTIFICATE", Bytes: issuerCert}); err != nil {
		return err
	}
	chainf.Close()

	// The "fullchain" file includes both the leaf cert and the issuer
	// (chain) cert.  There's nothing to indicate to the casual reader
	// of the file which is which, but this is the order in which certbot
	// writes them.
	fullchainf, err := os.Create(paths.FullChain)
	if err != nil {
		return err
	}
	if err = pem.Encode(fullchainf, &pem.Block{Type: "CERTIFICATE", Bytes: cert}); err != nil {
		return err
	}
	if err = pem.Encode(fullchainf, &pem.Block{Type: "CERTIFICATE", Bytes: issuerCert}); err != nil {
		return err
	}
	fullchainf.Close()

	// Probably not necessary, but just in case.
	x509Cert, err := validateCertificate(paths, time.Now())
	if err != nil {
		return err
	}

	// We don't want to keep old material around for ever; it'll build up.
	// Read the link to find the target; if the readlink fails, assume it's
	// not a link and just remove the path itself so we can put a link in
	// its place.
	cur := plat.ExpandDirPath(pathSSLDir)
	oldTarget, err := os.Readlink(cur)
	if err != nil {
		os.RemoveAll(cur)
	}

	// ln -s <hash> /.../<hash>.cur; mv /.../<hash>.cur /.../cur
	linkName := tmpDir + ".cur"
	if err = os.Symlink(filepath.Base(tmpDir), linkName); err != nil {
		return err
	}
	if err = os.Rename(linkName, cur); err != nil {
		return err
	}
	// Remove the old directory, if there was one, but only if it's not the
	// same as the new one.
	if oldTarget != filepath.Base(tmpDir) && oldTarget != "" {
		_ = os.RemoveAll(filepath.Join(filepath.Dir(tmpDir), oldTarget))
	}

	if config != nil {
		return sendCertRenewEvent(config, certFP, x509Cert, "cloud")
	}

	return nil
}

// GetKeyCertPaths will attempt to validate the preferred SSL certificate and
// private key on the appliance.  If valid, the full pathnames for each of these
// files will be returned.  If absent or invalid, GetKeyCertPaths will generate
// a self-signed certificate and new private key and return those pathnames.  If
// forceRefresh is true, then the cloud-retrieved and Let's Encrypt certificates
// are ignored and a new self-signed certificate is unconditionally generated.
func GetKeyCertPaths(config *cfgapi.Handle, domainname string, at time.Time,
	forceRefresh bool) (*CertPaths, error) {
	if !forceRefresh {
		paths := getCloudKeyCertPaths(plat)
		_, err := validateCertificate(paths, at)
		if err == nil {
			log.Printf("Cloud cert path found: %s", paths.Cert)
			return paths, nil
		}
		log.Printf("Cloud certs not available: %v", err)

		paths = getLEKeyCertPaths(domainname)
		_, err = validateCertificate(paths, at)
		if err == nil {
			return paths, nil
		}
		log.Printf("Manually installed Let's Encrypt certs not available: %v", err)
		// Try again with the explicit hostname "gateway", which is what
		// older installations used.
		paths = getLEKeyCertPaths("gateway." + domainname)
		_, err = validateCertificate(paths, at)
		if err == nil {
			return paths, nil
		}
		log.Printf("Manually installed Let's Encrypt certs (old path) not available: %v", err)
	}

	return createSSKeyCert(config, applianceTempCryptoDir, domainname, at,
		forceRefresh)
}

func init() {
	plat = platform.NewPlatform()
}
