package tlsconfig

import (
	"crypto/x509"
	"fmt"
	"time"
)

// verifyChain verifies certs[0] against roots, with the rest of certs as intermediates, for usage, at now's time or the
// current one when now is nil, and against name when it isn't empty. It's what crypto/tls does natively with RootCAs
// and ClientCAs. certs must not be empty.
func verifyChain(
	certs []*x509.Certificate,
	roots *x509.CertPool,
	name string,
	usage x509.ExtKeyUsage,
	now func() time.Time,
) error {
	opts := x509.VerifyOptions{
		Roots:         roots,
		DNSName:       name,
		Intermediates: x509.NewCertPool(),
		KeyUsages:     []x509.ExtKeyUsage{usage},
	}

	if now != nil {
		opts.CurrentTime = now()
	}

	for _, cert := range certs[1:] {
		opts.Intermediates.AddCert(cert)
	}

	_, err := certs[0].Verify(opts)

	return err
}

// parseCertificates parses the DER certificates a peer presented, as VerifyPeerCertificate is handed them.
func parseCertificates(raw [][]byte) ([]*x509.Certificate, error) {
	certs := make([]*x509.Certificate, 0, len(raw))

	for _, der := range raw {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("parsing peer certificate: %w", err)
		}

		certs = append(certs, cert)
	}

	return certs, nil
}
