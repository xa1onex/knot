package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

func outboundTLS() (*tls.Config, error) {
	caFile := os.Getenv("KNOT_TLS_CA")
	insecure := boolEnvLocal("KNOT_TLS_INSECURE", false)
	if caFile == "" && !insecure {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecure {
		cfg.InsecureSkipVerify = true
	}
	if caFile == "" {
		return cfg, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("KNOT_TLS_CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("KNOT_TLS_CA: no certificates in %s", caFile)
	}
	cfg.RootCAs = pool
	return cfg, nil
}

func outboundHTTP() (*http.Client, error) {
	tlsCfg, err := outboundTLS()
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if tlsCfg != nil {
		tr.TLSClientConfig = tlsCfg
	}
	return &http.Client{Transport: tr, Timeout: 30 * time.Second}, nil
}

func boolEnvLocal(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}
