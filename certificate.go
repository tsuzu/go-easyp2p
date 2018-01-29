package easyp2p

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

type GetCertificateFuncType func() (tls.Certificate, error)

func NewSelfSignedCertificate() (out tls.Certificate, retErr error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		retErr = err

		return
	}

	template := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			Organization: []string{"easyp2p"},
		},
		NotBefore: time.Now().AddDate(-1, 0, 0),
		NotAfter:  time.Now().AddDate(10, 0, 0),

		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		retErr = err

		return
	}

	certOut, keyOut := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		retErr = err

		return
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}); err != nil {
		retErr = err

		return
	}

	out, retErr = tls.X509KeyPair(certOut.Bytes(), keyOut.Bytes())

	return
}
