# Design : support LDAP dans la CRD Pooler

Ce document décrit le design détaillé pour ajouter l’authentification LDAP au Pooler CloudNativePG, en respectant les règles du projet (`.cursor/rules.md`) et les conventions existantes.

---

## 1. Structure Go exacte à ajouter

### 1.1 Emplacement

- **Fichier** : `api/v1/pooler_types.go`
- **Nouveau champ** : dans `PoolerSpec`, un champ optionnel `LDAP` de type `*PoolerLDAPConfig`.

### 1.2 Types à définir

```go
// PoolerLDAPConfig holds LDAP authentication settings for PgBouncer.
// When enabled, PgBouncer will use auth_type=ldap and validate client
// credentials against the configured LDAP server. LDAP is mutually
// exclusive with spec.pgbouncer.authQuery / authQuerySecret.
// +optional
type PoolerLDAPConfig struct {
	// Enable LDAP authentication. When true, auth_query is ignored and
	// PgBouncer uses LDAP for client authentication. Default: false.
	// +kubebuilder:default:=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Host is the LDAP server hostname or IP (e.g. "ldap.example.com").
	// Required when enabled is true.
	// +optional
	Host string `json:"host,omitempty"`

	// Port is the LDAP server port. Default: 389 (636 for LDAPS when TLS is enabled).
	// +kubebuilder:default:=389
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port *int32 `json:"port,omitempty"`

	// BaseDN is the base DN for LDAP search (e.g. "dc=example,dc=com").
	// Required when enabled is true.
	// +optional
	BaseDN string `json:"baseDN,omitempty"`

	// BindDN is the DN used to bind to LDAP for the search (e.g. "cn=admin,dc=example,dc=com").
	// Required when enabled is true.
	// +optional
	BindDN string `json:"bindDN,omitempty"`

	// SearchFilter is the LDAP filter for user lookup. Use %u for the client username.
	// Default: "(uid=%u)".
	// +kubebuilder:default:="(uid=%u)"
	// +optional
	SearchFilter string `json:"searchFilter,omitempty"`

	// TLS configures LDAP connection TLS (StartTLS or LDAPS).
	// +optional
	TLS *PoolerLDAPTLSConfig `json:"tls,omitempty"`

	// Credentials references the Secret containing bind password for BindDN.
	// The Secret must contain key "password" (or "bindPassword"). Required when enabled is true.
	// +optional
	Credentials *PoolerLDAPCredentials `json:"credentials,omitempty"`
}

// PoolerLDAPTLSConfig holds TLS options for the LDAP connection.
type PoolerLDAPTLSConfig struct {
	// Enable TLS for LDAP (StartTLS or LDAPS depending on port). Default: false.
	// +kubebuilder:default:=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SkipVerify disables verification of the LDAP server certificate. Default: false.
	// +kubebuilder:default:=false
	// +optional
	SkipVerify bool `json:"skipVerify,omitempty"`
}

// PoolerLDAPCredentials references the Kubernetes Secret holding LDAP bind credentials.
type PoolerLDAPCredentials struct {
	// Name of the Secret in the same namespace as the Pooler.
	// Expected key: "password" or "bindPassword".
	// +optional
	SecretName string `json:"secretName,omitempty"`
}
```

### 1.3 Modification de `PoolerSpec`

Ajouter après le champ `PgBouncer` (pour garder la cohérence avec les règles qui parlent de `spec.ldap`) :

```go
	// The PgBouncer configuration
	PgBouncer *PgBouncerSpec `json:"pgbouncer"`

	// LDAP configuration for PgBouncer client authentication. When enabled,
	// PgBouncer uses LDAP instead of auth_query. Mutually exclusive with
	// pgbouncer.authQuery / pgbouncer.authQuerySecret.
	// +optional
	LDAP *PoolerLDAPConfig `json:"ldap,omitempty"`

	// The deployment strategy to use for pgbouncer...
```

### 1.4 Constantes recommandées

Dans `api/v1/pooler_types.go` (ou un fichier dédié si préféré) :

```go
const (
	// DefaultLDAPPort is the default LDAP port (non-TLS).
	DefaultLDAPPort = 389
	// DefaultLDAPSearchFilter is the default LDAP filter for user search.
	DefaultLDAPSearchFilter = "(uid=%u)"
)
```

---

## 2. Mapping YAML final (exemple utilisateur)

```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Pooler
metadata:
  name: my-pooler
  namespace: my-namespace
spec:
  cluster:
    name: my-cluster
  type: rw
  instances: 2
  pgbouncer:
    poolMode: session
    # authQuery / authQuerySecret ne doivent pas être renseignés si ldap.enabled=true
  ldap:
    enabled: true
    host: ldap.example.com
    port: 389                    # optionnel, défaut 389
    baseDN: dc=example,dc=com
    bindDN: cn=pgbouncer,ou=users,dc=example,dc=com
    searchFilter: "(uid=%u)"      # optionnel, défaut "(uid=%u)"
    tls:
      enabled: false              # optionnel, défaut false
      skipVerify: false           # optionnel, défaut false
    credentials:
      secretName: pooler-ldap-bind-secret
```

Exemple minimal quand LDAP est activé (avec valeurs par défaut implicites) :

```yaml
spec:
  ldap:
    enabled: true
    host: ldap.example.com
    baseDN: dc=example,dc=com
    bindDN: cn=admin,dc=example,dc=com
    credentials:
      secretName: my-ldap-secret
  # port, searchFilter, tls omis → defaults appliqués (389, "(uid=%u)", tls disabled)
```

---

## 3. Champs optionnels vs obligatoires

| Champ | Obligatoire | Quand | Valeur par défaut |
|-------|-------------|--------|-------------------|
| `spec.ldap` | Non | Toujours optionnel (bloc entier) | Absent = pas de LDAP |
| `spec.ldap.enabled` | Non | Si `ldap` présent | `false` |
| `spec.ldap.host` | Oui | Si `ldap.enabled == true` | — |
| `spec.ldap.port` | Non | Si `ldap.enabled == true` | `389` |
| `spec.ldap.baseDN` | Oui | Si `ldap.enabled == true` | — |
| `spec.ldap.bindDN` | Oui | Si `ldap.enabled == true` | — |
| `spec.ldap.searchFilter` | Non | Si `ldap.enabled == true` | `"(uid=%u)"` |
| `spec.ldap.tls` | Non | Toujours optionnel | Sous-champs à défaut |
| `spec.ldap.tls.enabled` | Non | Si `tls` présent | `false` |
| `spec.ldap.tls.skipVerify` | Non | Si `tls` présent | `false` |
| `spec.ldap.credentials` | Oui | Si `ldap.enabled == true` | — |
| `spec.ldap.credentials.secretName` | Oui | Si `ldap.enabled == true` | — |

Règle métier (webhook) : **LDAP et auth_query sont mutuellement exclusifs**. Si `ldap.enabled == true`, alors `spec.pgbouncer.authQuery` et `spec.pgbouncer.authQuerySecret` doivent être vides / non renseignés (et inversement).

---

## 4. Valeurs par défaut

- **CRD (kubebuilder)** : utiliser `+kubebuilder:default:=...` pour `enabled`, `port`, `searchFilter`, `tls.enabled`, `tls.skipVerify` comme dans les structs ci-dessus.
- **Webhook / defaulting** : pour les champs sans marqueur CRD (ex. `port` quand on veut 389), appliquer les mêmes valeurs dans le webhook mutating ou dans une fonction `SetPoolerLDAPDefaults()` appelée avant validation, afin que la spec stockée soit toujours complète et cohérente.
- **Comportement** : si `spec.ldap` est absent ou si `spec.ldap.enabled == false`, aucun changement par rapport au comportement actuel (auth_query / auth_type hba, etc.). Donc **par défaut global : pas de LDAP**.

---

## 5. Contraintes de rétrocompatibilité

1. **Absence de `spec.ldap`**  
   - Comportement inchangé : auth via auth_query / Cluster comme aujourd’hui.  
   - Aucune migration requise pour les Pooler existants.

2. **`spec.ldap` présent mais `enabled: false`**  
   - Traité comme “LDAP désactivé”. Même comportement que l’absence de `ldap`.  
   - Permet d’activer plus tard en passant `enabled: true` sans toucher au reste de la spec.

3. **Pas de champs requis au niveau racine**  
   - Aucun champ obligatoire dans `PoolerSpec` n’est ajouté. Seuls des champs sont requis **conditionnellement** quand `ldap.enabled == true`, et la validation (webhook) rejette les specs invalides avec un message clair.

4. **Anciennes versions du controller**  
   - Un controller qui ne connaît pas `spec.ldap` ignore le champ (standard Kubernetes). Aucun impact sur le comportement actuel.  
   - Lors de la mise à jour du controller, les Pooler sans `ldap` ou avec `ldap.enabled: false` restent inchangés.

5. **Sérialisation**  
   - Tous les nouveaux champs sont `+optional` et avec `omitempty` en JSON, donc les manifests existants (sans `ldap`) restent valides et ne voient pas de champs ajoutés.

6. **Mutual exclusion**  
   - La règle “LDAP et auth_query mutuellement exclusifs” est appliquée au moment de la validation (webhook). Un utilisateur qui active LDAP doit retirer auth_query/authQuerySecret ; sinon la création/mise à jour est rejetée. Cela évite des états ambigus tout en restant rétrocompatible pour tous les Pooler qui n’utilisent pas LDAP.

---

## 6. Résumé des règles Cursor respectées

- **spec.ldap optionnel** : bloc entier optionnel, pas de breaking change.  
- **Secrets** : uniquement référence par `credentials.secretName` (Secret Kubernetes), pas de données sensibles dans la CRD.  
- **Webhook** : LDAP vs auth_query exclusifs ; si `ldap.enabled=true`, host, baseDN, bindDN, credentials.secretName requis ; défauts pour port et searchFilter ; validation TLS (ex. rejet si skipVerify avec des incohérences si on ajoute des règles métier).  
- **Opérateur** : génération config PgBouncer LDAP uniquement si activé ; montage du Secret ; pas de log de valeurs sensibles ; redémarrage/rolling restart sur changement de config LDAP.  
- **PgBouncer** : `auth_type=ldap` uniquement quand LDAP activé ; sinon conserver les valeurs par défaut existantes.

Ce design reste aligné avec l’architecture Pooler CRD → webhook → réconciliation → générateur de config PgBouncer et avec les conventions CloudNativePG (LocalObjectReference / SecretName, kubebuilder, optionalité).
