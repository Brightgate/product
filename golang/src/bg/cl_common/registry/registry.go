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

// NewAppliance registers a new appliance in the registry.  It returns the UUID
// it generated, as well as the private and public PEM-encoded keys.
func NewAppliance(ctx context.Context, db appliancedb.DataStore, project, region, regID, appID string) (uuid.UUID, []byte, []byte, error) {
	u := uuid.NewV4()

	keyPEM, certPEM, err := NewApplianceWithUUID(ctx, db, u, project, region, regID, appID)

	return u, keyPEM, certPEM, err
}

// NewApplianceWithUUID registers a new appliance like NewAppliance, but
// provides the UUID.
func NewApplianceWithUUID(ctx context.Context, db appliancedb.DataStore, u uuid.UUID, project, region, regID, appID string) ([]byte, []byte, error) {
	keyPEM, certPEM, err := genPEMKey()
	if err != nil {
		return nil, nil, err
	}

	id := &appliancedb.ApplianceID{
		CloudUUID:      u,
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
		return nil, nil, err
	}
	defer tx.Rollback()
	if err = db.InsertApplianceIDTx(ctx, tx, id); err != nil {
		return nil, nil, err
	}
	if err = db.InsertApplianceKeyTx(ctx, tx, u, key); err != nil {
		return nil, nil, err
	}
	err = tx.Commit()
	if err != nil {
		return nil, nil, err
	}
	return keyPEM, certPEM, nil
}
