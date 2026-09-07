package e2bapi

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"

	ecerrors "github.com/aliyun/elastic-compute-control-cli/pkg/errors"
)

func clientWithCA(path string) (*http.Client, error) {
	client := &http.Client{}
	if path == "" {
		return client, nil
	}
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, invalidCA()
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, invalidCA()
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, invalidCA()
	}
	// Clone rather than mutate the shared transport; preserve proxy and
	// connection defaults, and keep hostname verification enabled.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots}
	client.Transport = transport
	return client, nil
}

func invalidCA() error {
	return ecerrors.Client("InvalidSandboxCA", e2bMessage("InvalidSandboxCA"), ecerrors.WithField("ECCTL_SANDBOX_CA_FILE"))
}
