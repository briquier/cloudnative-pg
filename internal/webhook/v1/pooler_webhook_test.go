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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pooler validation", func() {
	var v *PoolerCustomValidator
	BeforeEach(func() {
		v = &PoolerCustomValidator{}
	})

	It("doesn't allow specifying authQuerySecret without any authQuery", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuerySecret: &apiv1.LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).NotTo(BeEmpty())
	})

	It("doesn't allow specifying authQuery without any authQuerySecret", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "test",
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).NotTo(BeEmpty())
	})

	It("allows having both authQuery and authQuerySecret", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "test",
					AuthQuerySecret: &apiv1.LocalObjectReference{
						Name: "test",
					},
				},
			},
		}

		Expect(v.validatePgBouncer(pooler)).To(BeEmpty())
	})

	It("allows the autoconfiguration mode", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{},
			},
		}

		Expect(v.validatePgBouncer(pooler)).To(BeEmpty())
	})

	It("doesn't allow not specifying a cluster name", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: ""},
			},
		}
		Expect(v.validateCluster(pooler)).NotTo(BeEmpty())
	})

	It("doesn't allow to have a pooler with the same name of the cluster", func() {
		pooler := &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{
					Name: "test",
				},
			},
		}
		Expect(v.validateCluster(pooler)).NotTo(BeEmpty())
	})

	It("doesn't complain when specifying a cluster name", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster-example"},
			},
		}
		Expect(v.validateCluster(pooler)).To(BeEmpty())
	})

	It("does complain when given a fixed parameter", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Parameters: map[string]string{"pool_mode": "test"},
				},
			},
		}
		Expect(v.validatePgbouncerGenericParameters(pooler)).NotTo(BeEmpty())
	})

	It("does not complain when given a valid parameter", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				PgBouncer: &apiv1.PgBouncerSpec{
					Parameters: map[string]string{"verbose": "10"},
				},
			},
		}
		Expect(v.validatePgbouncerGenericParameters(pooler)).To(BeEmpty())
	})
})

var _ = Describe("Pooler LDAP validation", func() {
	var v *PoolerCustomValidator
	BeforeEach(func() {
		v = &PoolerCustomValidator{}
	})

	It("rejects LDAP enabled with auth_query set", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BaseDN:     "dc=example,dc=com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		pooler.Spec.PgBouncer.AuthQuery = "SELECT 1"
		pooler.Spec.PgBouncer.AuthQuerySecret = &apiv1.LocalObjectReference{Name: "auth-secret"}
		Expect(v.validateLDAP(pooler)).NotTo(BeEmpty())
		Expect(v.validateLDAP(pooler)[0].Detail).To(ContainSubstring("mutually exclusive"))
	})

	It("rejects LDAP enabled when host is missing", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					BaseDN:     "dc=example,dc=com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		Expect(v.validateLDAP(pooler)).NotTo(BeEmpty())
	})

	It("rejects LDAP enabled when credentials.secretName is missing", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled: true,
					Host:    "ldap.example.com",
					BaseDN:  "dc=example,dc=com",
					BindDN:  "cn=admin,dc=example,dc=com",
				},
			},
		}
		Expect(v.validateLDAP(pooler)).NotTo(BeEmpty())
	})

	It("accepts valid LDAP config when enabled", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BaseDN:     "dc=example,dc=com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		Expect(v.validateLDAP(pooler)).To(BeEmpty())
	})

	It("does not validate LDAP when ldap is nil or disabled", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
			},
		}
		Expect(v.validateLDAP(pooler)).To(BeEmpty())
		pooler.Spec.LDAP = &apiv1.PoolerLDAPConfig{Enabled: false}
		Expect(v.validateLDAP(pooler)).To(BeEmpty())
	})

	It("rejects LDAP enabled when baseDN is missing", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		Expect(v.validateLDAP(pooler)).NotTo(BeEmpty())
	})

	It("rejects LDAP enabled when bindDN is missing", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BaseDN:     "dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		Expect(v.validateLDAP(pooler)).NotTo(BeEmpty())
	})
})

var _ = Describe("Pooler full validation with LDAP", func() {
	var v *PoolerCustomValidator
	BeforeEach(func() {
		v = &PoolerCustomValidator{}
	})

	It("accepts valid Pooler with LDAP enabled (validate returns no errors)", func() {
		pooler := &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pooler"},
			Spec: apiv1.PoolerSpec{
				Cluster:   apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BaseDN:     "dc=example,dc=com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		Expect(v.validate(pooler)).To(BeEmpty())
	})

	It("rejects Pooler with LDAP and auth_query set (validate returns errors)", func() {
		pooler := &apiv1.Pooler{
			ObjectMeta: metav1.ObjectMeta{Name: "my-pooler"},
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "my-cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "SELECT 1",
					AuthQuerySecret: &apiv1.LocalObjectReference{Name: "auth-secret"},
				},
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:    true,
					Host:       "ldap.example.com",
					BaseDN:     "dc=example,dc=com",
					BindDN:     "cn=admin,dc=example,dc=com",
					Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
				},
			},
		}
		errs := v.validate(pooler)
		Expect(errs).NotTo(BeEmpty())
		Expect(errs.ToAggregate().Error()).To(ContainSubstring("mutually exclusive"))
	})
})

var _ = Describe("Pooler LDAP defaulting", func() {
	It("sets port and searchFilter when LDAP enabled and not set", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled: true,
					Host:    "ldap.example.com",
				},
			},
		}
		setPoolerLDAPDefaults(pooler)
		Expect(pooler.Spec.LDAP.Port).NotTo(BeNil())
		Expect(*pooler.Spec.LDAP.Port).To(Equal(int32(apiv1.DefaultLDAPPort)))
		Expect(pooler.Spec.LDAP.SearchFilter).To(Equal(apiv1.DefaultLDAPSearchFilter))
	})

	It("does not overwrite port or searchFilter when already set", func() {
		customPort := int32(636)
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				LDAP: &apiv1.PoolerLDAPConfig{
					Enabled:      true,
					Port:         ptr.To(customPort),
					SearchFilter: "(cn=%u)",
				},
			},
		}
		setPoolerLDAPDefaults(pooler)
		Expect(*pooler.Spec.LDAP.Port).To(Equal(customPort))
		Expect(pooler.Spec.LDAP.SearchFilter).To(Equal("(cn=%u)"))
	})

	It("does nothing when LDAP is nil or disabled", func() {
		pooler := &apiv1.Pooler{Spec: apiv1.PoolerSpec{}}
		setPoolerLDAPDefaults(pooler)
		Expect(pooler.Spec.LDAP).To(BeNil())

		pooler.Spec.LDAP = &apiv1.PoolerLDAPConfig{Enabled: false}
		setPoolerLDAPDefaults(pooler)
		Expect(pooler.Spec.LDAP.Port).To(BeNil())
		Expect(pooler.Spec.LDAP.SearchFilter).To(BeEmpty())
	})
})

var _ = Describe("Pooler HBA mode with mixed authentication", func() {
	v := &PoolerCustomValidator{}

	ldapConfig := func() *apiv1.PoolerLDAPConfig {
		return &apiv1.PoolerLDAPConfig{
			Enabled:     true,
			Host:        "ldap.example.com",
			BaseDN:      "dc=example,dc=com",
			BindDN:      "cn=admin,dc=example,dc=com",
			Credentials: &apiv1.PoolerLDAPCredentials{SecretName: "ldap-secret"},
		}
	}

	It("allows LDAP + authQuery when pg_hba rules are defined (HBA mode)", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "SELECT usename, passwd FROM pg_shadow WHERE usename=$1",
					AuthQuerySecret: &apiv1.LocalObjectReference{
						Name: "auth-secret",
					},
					PgHBA: []string{
						"host tutu ldap-user 0.0.0.0/0 ldap",
						"host all basic-user 0.0.0.0/0 scram-sha-256",
					},
				},
				LDAP: ldapConfig(),
			},
		}
		Expect(v.validateLDAP(pooler)).To(BeEmpty(),
			"LDAP + authQuery should be allowed when pg_hba rules are defined")
	})

	It("rejects LDAP + authQuery when pg_hba is empty (pure LDAP mode)", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					AuthQuery: "SELECT usename, passwd FROM pg_shadow WHERE usename=$1",
				},
				LDAP: ldapConfig(),
			},
		}
		errs := v.validateLDAP(pooler)
		Expect(errs).NotTo(BeEmpty(), "LDAP + authQuery should be rejected in pure LDAP mode")
	})

	It("rejects pg_hba rule with ldap method when spec.ldap is not configured", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					PgHBA: []string{"host all ldap-user 0.0.0.0/0 ldap"},
				},
			},
		}
		errs := v.validatePgBouncerHBA(pooler)
		Expect(errs).NotTo(BeEmpty(), "pg_hba ldap rule without spec.ldap should be rejected")
	})

	It("accepts pg_hba rule with ldap method when spec.ldap is configured", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					PgHBA: []string{"host all ldap-user 0.0.0.0/0 ldap"},
				},
				LDAP: ldapConfig(),
			},
		}
		Expect(v.validatePgBouncerHBA(pooler)).To(BeEmpty())
	})

	It("accepts pg_hba rules without ldap method regardless of spec.ldap", func() {
		pooler := &apiv1.Pooler{
			Spec: apiv1.PoolerSpec{
				Cluster: apiv1.LocalObjectReference{Name: "cluster"},
				PgBouncer: &apiv1.PgBouncerSpec{
					PgHBA: []string{
						"host all basic-user 0.0.0.0/0 scram-sha-256",
						"host all user-b 0.0.0.0/0 md5",
					},
				},
			},
		}
		Expect(v.validatePgBouncerHBA(pooler)).To(BeEmpty())
	})
})
