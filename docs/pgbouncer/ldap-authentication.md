# LDAP authentication with PgBouncer

<!-- SPDX-License-Identifier: CC-BY-4.0 -->

CloudNativePG supports **LDAP** authentication for client connections to PgBouncer. When LDAP is enabled, PgBouncer validates client credentials (username and password) against an LDAP server instead of querying PostgreSQL via `auth_query`.

## How it works

1. **Without LDAP** (default behavior)  
   PgBouncer authenticates either via an SQL query (`auth_query` and `authQuerySecret`) or via other mechanisms configured at the Cluster level. Users are verified against PostgreSQL.

2. **With LDAP**  
   When `spec.ldap.enabled` is `true`:
   - PgBouncer uses `auth_type=ldap`.
   - On each client connection, PgBouncer connects to the LDAP server with a service account (`bindDN` and password from a Kubernetes Secret).
   - It performs an LDAP search (base DN and filter, with `%u` for the client username).
   - It attempts an LDAP bind with the found DN and the password supplied by the client.
   - If the bind succeeds, the connection to the pooler is accepted; PgBouncer then establishes the connection to PostgreSQL (using the credentials configured for the Cluster/pooler backend).

!!! Important "LDAP / auth_query mutual exclusivity"
    LDAP and `auth_query` are **mutually exclusive**. If `spec.ldap.enabled` is `true`, you must not set `spec.pgbouncer.authQuery` or `spec.pgbouncer.authQuerySecret`. Conversely, if you use `auth_query`, do not configure LDAP. The admission webhook rejects a spec that would contain both.

The operator:
- generates the PgBouncer configuration with `auth_type=ldap` and LDAP options (URL, bind DN, path to the file containing the bind password);
- mounts the Secret containing the bind password read-only in the PgBouncer Pod;
- never logs the password; only paths and Secret names are used in the config.

## Complete Pooler example with LDAP

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
    parameters:
      max_client_conn: "500"
      default_pool_size: "25"
  # Do not set authQuery or authQuerySecret when LDAP is enabled.
  ldap:
    enabled: true
    host: ldap.example.com
    port: 636
    baseDN: dc=example,dc=com
    bindDN: cn=pgbouncer,ou=users,dc=example,dc=com
    searchFilter: "(uid=%u)"
    tls:
      enabled: true
      skipVerify: false
    credentials:
      secretName: pooler-ldap-bind-secret
```

Field reference:
- **host** (required when LDAP is enabled): LDAP server hostname.
- **port** (optional): Port (default `389` without TLS; `636` is commonly used with TLS).
- **baseDN** (required): LDAP search base (e.g. `dc=example,dc=com`).
- **bindDN** (required): DN used to bind to LDAP to perform the search (service account).
- **searchFilter** (optional): LDAP filter to find the user entry; `%u` is replaced by the client username (default: `(uid=%u)`).
- **tls.enabled** (optional): Enable TLS (LDAPS or StartTLS depending on port). Default: `false`.
- **tls.skipVerify** (optional): Do not verify the LDAP server certificate. Default: `false`. Avoid in production.
- **credentials.secretName** (required when LDAP is enabled): Name of the Secret containing the `bindDN` password.

## Secret example for LDAP bind

The Secret must be in the **same namespace** as the Pooler. The expected key is **`password`** (value = password for the `bindDN` account).

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: pooler-ldap-bind-secret
  namespace: my-namespace
type: Opaque
stringData:
  password: "your-ldap-bind-password"
```

!!! Warning
    Do not commit this Secret with a plain-text password. Use a secrets manager (e.g. [External Secrets](https://cloudnative-pg.github.io/cloudnative-pg/latest/cncf-projects/external-secrets/)) or inject the password via a CI/CD pipeline.

The operator mounts this Secret read-only in the PgBouncer Pod (e.g. at `/etc/pgbouncer-ldap/bind.password`). Only PgBouncer reads this file; it does not appear in exposed configuration (logs, ConfigMap, etc.).

## Security considerations

1. **LDAP bind Secret**
   - Store the `bindDN` password only in a Kubernetes Secret (or equivalent secure store). Never put it in the Pooler spec.
   - Restrict RBAC access to the Secret (only the Pooler's ServiceAccount and operators need read access).
   - The volume is mounted with restrictive permissions (read-only, e.g. 0400).

2. **TLS**
   - In production, enable `ldap.tls.enabled` and use port 636 (LDAPS) or StartTLS.
   - Avoid `tls.skipVerify: true` except for temporary debugging; it exposes you to man-in-the-middle attacks.

3. **Network**
   - Ensure PgBouncer Pods can reach the LDAP server (network policies, Kubernetes network policy if needed).

4. **LDAP service account**
   - The `bindDN` should have only the rights needed for user search (read on the base DN and attributes used by the filter). Apply least privilege.

5. **No sensitive data in plain text**
   - The Pooler spec contains only references (Secret name, host, DN, filter). No password is written to ConfigMaps or annotations.

## Known limitations

- **PgBouncer compatibility**: The feature relies on PgBouncer's `auth_type=ldap` and LDAP options. Ensure the PgBouncer image version used by CloudNativePG supports LDAP authentication.
- **Single LDAP server**: One LDAP configuration per Pooler (one host/port, one base DN, one filter). No multi-server fallback is managed by the CRD.
- **Search filter**: The filter is global for all pooler users; the `%u` placeholder is replaced by the client username. More complex LDAP schemas (multiple OUs, roles) may require an adapted filter or constraints on the directory side.
- **No PostgreSQL user sync**: LDAP only authenticates access to PgBouncer. Users must exist in PostgreSQL (or be created by another mechanism) for backend connections to succeed. CloudNativePG does not automatically create PostgreSQL users from LDAP.
- **LDAP config changes**: Changing the LDAP spec (or the referenced Secret) triggers a rolling restart of PgBouncer Pods to apply the new configuration.

## Quick verification

After deploying the Pooler with LDAP:

1. Check that PgBouncer Pods are `Running` and that the LDAP Secret is mounted:
   ```bash
   kubectl get pooler -n my-namespace
   kubectl describe pod -n my-namespace -l cnpg.io/poolerName=my-pooler
   ```
2. Test a connection with a user whose account exists in both LDAP (matching the `(uid=%u)` filter) and PostgreSQL.

!!! Seealso
    - [Connection pooling](../src/connection_pooling.md) for general Pooler concepts.
    - [API reference](../src/cloudnative-pg.v1.md) for the full definition of `Pooler` and `PoolerLDAPConfig`.
