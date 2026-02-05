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
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// minimalSecrets returns Secrets with the minimal data required for BuildConfigurationFiles
// (BasicAuth for AuthQuery, placeholder cert data for ServerCA/Client/ClientCA).
func minimalSecrets() *Secrets {
	return &Secrets{
		AuthQuery: &corev1.Secret{
			Type: corev1.SecretTypeBasicAuth,
			Data: map[string][]byte{
				corev1.BasicAuthUsernameKey: []byte("authuser"),
				corev1.BasicAuthPasswordKey: []byte("authpass"),
			},
		},
		ServerCA: &corev1.Secret{
			Data: map[string][]byte{certs.CACertKey: []byte("ca-cert")},
		},
		Client: &corev1.Secret{
			Data: map[string][]byte{
				certs.TLSCertKey:       []byte("tls-cert"),
				certs.TLSPrivateKeyKey: []byte("tls-key"),
			},
		},
		ClientCA: &corev1.Secret{
			Data: map[string][]byte{certs.CACertKey: []byte("client-ca")},
		},
	}
}

// poolerWithoutLDAP returns a Pooler with no LDAP config (retrocompat).
func poolerWithoutLDAP() *apiv1.Pooler {
	return &apiv1.Pooler{
		Spec: apiv1.PoolerSpec{
			Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
			PgBouncer: &apiv1.PgBouncerSpec{},
		},
	}
}

// poolerWithLDAP returns a Pooler with LDAP enabled and required fields set.
func poolerWithLDAP() *apiv1.Pooler {
	return &apiv1.Pooler{
		Spec: apiv1.PoolerSpec{
			Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
			PgBouncer: &apiv1.PgBouncerSpec{},
			LDAP: &apiv1.PoolerLDAPConfig{
				Enabled:      true,
				Host:         "ldap.example.com",
				Port:         ptr.To(int32(389)),
				BaseDN:       "dc=example,dc=com",
				BindDN:       "cn=admin,dc=example,dc=com",
				SearchFilter: apiv1.DefaultLDAPSearchFilter,
				Credentials:  &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
			},
		},
	}
}

var _ = Describe("PgBouncer config generation with LDAP", func() {
	It("when LDAP is enabled, generated ini contains auth_type=ldap and auth_ldap_options", func() {
		pooler := poolerWithLDAP()
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(BeNil())

		iniPath := filepath.Join(ConfigsDir, PgBouncerIniFileName)
		ini, ok := files[iniPath]
		Expect(ok).To(BeTrue())
		iniStr := string(ini)
		Expect(iniStr).To(ContainSubstring("auth_type = ldap"))
		Expect(iniStr).To(ContainSubstring("auth_ldap_options"))
		Expect(iniStr).To(ContainSubstring("ldapurl="))
		Expect(iniStr).To(ContainSubstring(GetLDAPBindPasswordFilePath()))
		Expect(iniStr).To(ContainSubstring("ldap.example.com"))
		Expect(iniStr).To(ContainSubstring("dc=example,dc=com"))
	})

	It("when LDAP is enabled, userlist.txt is not generated", func() {
		pooler := poolerWithLDAP()
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		userlistPath := filepath.Join(ConfigsDir, PgBouncerUserListFileName)
		_, ok := files[userlistPath]
		Expect(ok).To(BeFalse())
	})

	It("when LDAP is enabled with TLS, ldapurl uses ldaps scheme", func() {
		pooler := poolerWithLDAP()
		pooler.Spec.LDAP.TLS = &apiv1.PoolerLDAPTLSConfig{Enabled: true}
		pooler.Spec.LDAP.Port = ptr.To(int32(636))
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		iniPath := filepath.Join(ConfigsDir, PgBouncerIniFileName)
		iniStr := string(files[iniPath])
		Expect(iniStr).To(ContainSubstring("ldaps://ldap.example.com:636/"))
	})
})

var _ = Describe("PgBouncer config backward compatibility (LDAP absent)", func() {
	It("when LDAP is absent, generated ini contains auth_type hba and no auth_ldap_options", func() {
		pooler := poolerWithoutLDAP()
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).NotTo(BeNil())

		iniPath := filepath.Join(ConfigsDir, PgBouncerIniFileName)
		ini, ok := files[iniPath]
		Expect(ok).To(BeTrue())
		iniStr := string(ini)
		Expect(iniStr).To(ContainSubstring("auth_type = hba"))
		Expect(iniStr).NotTo(ContainSubstring("auth_type = ldap"))
		Expect(iniStr).NotTo(ContainSubstring("auth_ldap_options"))
	})

	It("when LDAP is absent, userlist.txt is generated", func() {
		pooler := poolerWithoutLDAP()
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		userlistPath := filepath.Join(ConfigsDir, PgBouncerUserListFileName)
		userlist, ok := files[userlistPath]
		Expect(ok).To(BeTrue())
		Expect(string(userlist)).To(ContainSubstring("authuser"))
	})

	It("when LDAP is present but disabled, behavior is same as LDAP absent", func() {
		pooler := poolerWithoutLDAP()
		pooler.Spec.LDAP = &apiv1.PoolerLDAPConfig{Enabled: false}
		secrets := minimalSecrets()
		files, err := BuildConfigurationFiles(pooler, secrets)
		Expect(err).NotTo(HaveOccurred())
		iniPath := filepath.Join(ConfigsDir, PgBouncerIniFileName)
		iniStr := string(files[iniPath])
		Expect(iniStr).To(ContainSubstring("auth_type = hba"))
		Expect(iniStr).NotTo(ContainSubstring("auth_ldap_options"))
		userlistPath := filepath.Join(ConfigsDir, PgBouncerUserListFileName)
		_, ok := files[userlistPath]
		Expect(ok).To(BeTrue())
	})
})

var _ = Describe("LDAP URL and options building", func() {
	It("buildLDAPURL produces RFC 4516 style URL with default port and filter", func() {
		pooler := poolerWithLDAP()
		pooler.Spec.LDAP.SearchFilter = "" // use default
		url, err := buildLDAPURL(pooler)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(ContainSubstring("ldap://ldap.example.com:389/"))
		Expect(url).To(ContainSubstring("sub"))
		// Filter (uid=%u) is URL-encoded
		Expect(url).To(ContainSubstring("%28uid%3D%25u%29"))
	})

	It("buildLDAPURL uses ldaps and custom port when TLS enabled", func() {
		pooler := poolerWithLDAP()
		pooler.Spec.LDAP.TLS = &apiv1.PoolerLDAPTLSConfig{Enabled: true}
		pooler.Spec.LDAP.Port = ptr.To(int32(636))
		url, err := buildLDAPURL(pooler)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(HavePrefix("ldaps://"))
		Expect(url).To(ContainSubstring(":636/"))
	})

	It("applyLDAPParameters leaves params unchanged when LDAP disabled", func() {
		pooler := poolerWithoutLDAP()
		params := map[string]string{"auth_type": "hba", "verbose": "1"}
		err := applyLDAPParameters(pooler, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(params["auth_type"]).To(Equal("hba"))
		Expect(params).NotTo(HaveKey("auth_ldap_options"))
	})

	It("applyLDAPParameters sets auth_type and auth_ldap_options when LDAP enabled", func() {
		pooler := poolerWithLDAP()
		params := buildPgBouncerParameters(nil)
		err := applyLDAPParameters(pooler, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(params["auth_type"]).To(Equal("ldap"))
		Expect(params["auth_ldap_options"]).NotTo(BeEmpty())
		Expect(params["auth_ldap_options"]).To(ContainSubstring("ldapurl="))
		Expect(params["auth_ldap_options"]).To(ContainSubstring(GetLDAPBindPasswordFilePath()))
	})
})

var _ = Describe("GetLDAPBindPasswordFilePath", func() {
	It("returns path under LDAPBindPasswordMountDir", func() {
		p := GetLDAPBindPasswordFilePath()
		Expect(p).To(Equal(LDAPBindPasswordMountDir + "/" + LDAPBindPasswordFileName))
		Expect(strings.HasPrefix(p, LDAPBindPasswordMountDir)).To(BeTrue())
	})
})
