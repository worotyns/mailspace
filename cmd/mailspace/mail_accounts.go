// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mailspace.local/mailspace/internal/accounts"
	"mailspace.local/mailspace/internal/identity"
	"mailspace.local/mailspace/internal/mailnet"
	"os"
	"path/filepath"
)

func credentialKeysPath() string {
	if path := os.Getenv("MAILSPACE_CREDENTIAL_KEYS"); path != "" {
		return path
	}
	return filepath.Join(filepath.Dir(databasePath()), "credential-keys")
}
func mailConnector() (*mailnet.Connector, *mailnet.Policy, error) {
	policy, err := mailnet.NewPolicy(os.Getenv("MAILSPACE_MAIL_ENDPOINTS"), os.Getenv("MAILSPACE_PRIVATE_MAIL_ENDPOINTS"))
	if err != nil {
		return nil, nil, err
	}
	connector := &mailnet.Connector{Policy: policy}
	if path := os.Getenv("MAILSPACE_MAIL_CA_FILE"); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return nil, nil, errors.New("mail CA file must be a regular PEM file under 1 MiB")
		}
		pem, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		if err != nil {
			return nil, nil, err
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, nil, errors.New("mail CA file contains no usable certificate")
		}
		connector.Roots = roots
	}
	return connector, policy, nil
}
func rotateMailCredentials() error {
	store, err := identity.Open(databasePath())
	if err != nil {
		return err
	}
	defer store.Close()
	service, err := accounts.Open(store.Database(), credentialKeysPath(), nil)
	if err != nil {
		return err
	}
	id, err := service.RotateKey(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("Rotated IMAP/SMTP credential encryption key: %s. Retain previous key files in backups.\n", id)
	return nil
}
