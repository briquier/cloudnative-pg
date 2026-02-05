/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"fmt"
	"net/url"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// LDAPBindPasswordMountDir is the directory where the LDAP bind password Secret
// is mounted in the PgBouncer Pod. Used by the operator to add the volume;
// PgBouncer config references LDAPBindPasswordMountDir/LDAPBindPasswordFileName.
const LDAPBindPasswordMountDir = "/etc/pgbouncer-ldap"

// LDAPBindPasswordFileName is the file name of the mounted bind password in the Pod.
const LDAPBindPasswordFileName = "bind.password"

// isLDAPEnabled returns true when the Pooler has LDAP authentication enabled.
func isLDAPEnabled(pooler *apiv1.Pooler) bool {
	return pooler != nil && pooler.Spec.LDAP != nil && pooler.Spec.LDAP.Enabled
}

// buildLDAPURL builds the ldapurl value for auth_ldap_options (RFC 4516).
// Format: ldap://host:port/baseDN??scope?filter (filter is URL-encoded).
// No sensitive data is included; bind password is supplied via file by the controller.
func buildLDAPURL(pooler *apiv1.Pooler) (string, error) {
	ldap := pooler.Spec.LDAP
	if ldap == nil || !ldap.Enabled {
		return "", nil
	}
	port := apiv1.DefaultLDAPPort
	if ldap.Port != nil {
		port = int(*ldap.Port)
	}
	scheme := "ldap"
	if ldap.TLS != nil && ldap.TLS.Enabled {
		scheme = "ldaps"
	}
	filter := ldap.SearchFilter
	if filter == "" {
		filter = apiv1.DefaultLDAPSearchFilter
	}
	// LDAP URL: baseDN and filter must be percent-encoded (RFC 4516).
	encodedDN := url.PathEscape(ldap.BaseDN)
	encodedFilter := url.PathEscape(filter)
	ldapURL := fmt.Sprintf("%s://%s:%d/%s??sub?%s", scheme, ldap.Host, port, encodedDN, encodedFilter)
	return ldapURL, nil
}

// GetLDAPBindPasswordFilePath returns the path where the LDAP bind password
// file is read when LDAP is enabled. The Secret is mounted at this path by the
// operator; this package never handles the raw password.
func GetLDAPBindPasswordFilePath() string {
	return LDAPBindPasswordMountDir + "/" + LDAPBindPasswordFileName
}

// buildAuthLDAPOptions returns the value for the auth_ldap_options parameter.
// Only non-sensitive options are included (ldapurl, ldapbinddn, ldapbindpasswdfile path).
// The bind password is stored in a file by the controller; we only reference the path.
func buildAuthLDAPOptions(pooler *apiv1.Pooler) (string, error) {
	ldapURL, err := buildLDAPURL(pooler)
	if err != nil {
		return "", err
	}
	if ldapURL == "" {
		return "", nil
	}
	// PgBouncer: auth_ldap_options = ldapurl="..." ldapbinddn="..." ldapbindpasswdfile="..."
	opts := fmt.Sprintf("ldapurl=\"%s\"", ldapURL)
	if pooler.Spec.LDAP.BindDN != "" {
		opts += fmt.Sprintf(" ldapbinddn=\"%s\"", pooler.Spec.LDAP.BindDN)
	}
	opts += fmt.Sprintf(" ldapbindpasswdfile=\"%s\"", GetLDAPBindPasswordFilePath())
	return opts, nil
}

// applyLDAPParameters sets auth_type and auth_ldap_options when LDAP is enabled.
// When LDAP is disabled, parameters are left unchanged (no regression).
func applyLDAPParameters(pooler *apiv1.Pooler, parameters map[string]string) error {
	if !isLDAPEnabled(pooler) {
		return nil
	}
	opts, err := buildAuthLDAPOptions(pooler)
	if err != nil {
		return err
	}
	parameters["auth_type"] = "ldap"
	parameters["auth_ldap_options"] = opts
	return nil
}

