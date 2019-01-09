package registry

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math"
	"math/big"
	"time"

	"github.com/satori/uuid"

	"bg/cloud_models/appliancedb"
)

// PubSub is a part of ApplianceRegistry, describing the publisher/subscriber
// topic that has been set up for a registry.
type PubSub struct {
	Events string `json:"events"`
}

// ApplianceRegistry is the registry configuration that is used to configure new
// appliances.
type ApplianceRegistry struct {
	Project     string `json:"project"`
	Region      string `json:"region"`
	Registry    string `json:"registry"`
	SQLInstance string `json:"cloudsql_instance"`
	DbURI       string `json:"dburi"`
	PubSub      PubSub `json:"pubsub"`
}

func genPEMKey() ([]byte, []byte, error) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(30 * 24 * time.Hour)
	serialMax := big.NewInt(math.MaxInt64)
	serialNumber, err := rand.Int(rand.Reader, serialMax)
	if err != nil {
		return nil, nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "unused"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template,
		&template, &privKey.PublicKey, privKey)
	if err != nil {
		return nil, nil, err
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})

	return keyPEM, certPEM, nil
}

// NewSite registers a new site in the registry.  It returns the site UUID.
func NewSite(ctx context.Context, db appliancedb.DataStore, name string) (uuid.UUID, error) {
	u := uuid.NewV4()

	err := db.InsertCustomerSite(ctx, &appliancedb.CustomerSite{
		UUID: u,
		Name: name,
	})
	if err != nil {
		return uuid.Nil, err
	}
	return u, nil
}

// NewAppliance registers a new appliance.
// If appliance is uuid.Nil, a uuid is selected.
// If site is nil, a Site UUID will be picked automatically.
func NewAppliance(ctx context.Context, db appliancedb.DataStore,
	appliance uuid.UUID, site *uuid.UUID,
	project, region, regID, appID string) (uuid.UUID, uuid.UUID, []byte, []byte, error) {

	createSite := false
	keyPEM, certPEM, err := genPEMKey()
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, nil, err
	}

	if appliance == uuid.Nil {
		appliance = uuid.NewV4()
	}
	if site == nil {
		u := uuid.NewV4()
		site = &u
		createSite = true
	}

	id := &appliancedb.ApplianceID{
		ApplianceUUID:  appliance,
		SiteUUID:       *site,
		GCPProject:     project,
		GCPRegion:      region,
		ApplianceReg:   regID,
		ApplianceRegID: appID,
	}
	key := &appliancedb.AppliancePubKey{
		Format: "RS256_X509",
		Key:    string(certPEM),
	}

	tx, err := db.BeginTx(ctx)
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, nil, err
	}
	defer tx.Rollback()

	if createSite {
		s := appliancedb.CustomerSite{
			UUID: *site,
			Name: "",
		}
		if err = db.InsertCustomerSiteTx(ctx, tx, &s); err != nil {
			return uuid.Nil, uuid.Nil, nil, nil, err
		}
	}

	if err = db.InsertApplianceIDTx(ctx, tx, id); err != nil {
		return uuid.Nil, uuid.Nil, nil, nil, err
	}
	if err = db.InsertApplianceKeyTx(ctx, tx, appliance, key); err != nil {
		return uuid.Nil, uuid.Nil, nil, nil, err
	}
	err = tx.Commit()
	if err != nil {
		return uuid.Nil, uuid.Nil, nil, nil, err
	}
	return appliance, *site, keyPEM, certPEM, nil
}
