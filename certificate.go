package easyp2p

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// CertificateServerName is the default server name for the quic server
const CertificateServerName = "easyp2p.go"

// CertificateGeneratorType is the type of functions which create certificates.
// DNS names in the certificate of the server must be CertificateServerName(easyp2p.go)
type CertificateGeneratorType func() (CertificatesType, error)

// CertificatesType needs to contain a certificate of CA as PEM and a certificate created with the CA
type CertificatesType struct {
	CACertPEM []byte
	Cert      tls.Certificate
}

// NewSelfSignedCertificate is the default function of CertificateGeneratorType
func NewSelfSignedCertificate() (out CertificatesType, retErr error) {
	ca := &x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			Organization: []string{"easyp2p-ca"},
		},
		NotBefore: time.Now().AddDate(-1, 0, 0),
		NotAfter:  time.Now().AddDate(10, 0, 0),
		IsCA:      true,

		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	caPriv, _ := rsa.GenerateKey(rand.Reader, 2048)
	caX509, err := x509.CreateCertificate(rand.Reader, ca, ca, &caPriv.PublicKey, caPriv)
	if err != nil {
		retErr = err

		return
	}

	out.CACertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caX509})
	//caKeyOut := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caPriv)})

	template := &x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		DNSNames:     []string{CertificateServerName},
		Subject: pkix.Name{
			Organization: []string{"easyp2p"},
		},
		NotBefore: time.Now().AddDate(-1, 0, 0),
		NotAfter:  time.Now().AddDate(10, 0, 0),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	derBytes, err := x509.CreateCertificate(rand.Reader, template, ca, &priv.PublicKey, caPriv)
	if err != nil {
		retErr = err

		return
	}

	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	out.Cert, retErr = tls.X509KeyPair(certOut, keyOut)

	return
}
