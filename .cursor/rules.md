# Cursor Rules – CloudNativePG PgBouncer LDAP

## Project context

You are working on a fork of CloudNativePG (branch release-1.28).

Goal:
Add first-class support for LDAP authentication in PgBouncer, exposed natively via the Pooler CRD, admission webhook, operator reconciliation logic, and PgBouncer configuration generation.

Context:
- CloudNativePG 1.28 embeds PgBouncer >= 1.25, which supports LDAP.
- Upstream does not expose LDAP configuration in the Pooler CRD yet.
- This fork must remain upstream-friendly, clean, and maintainable.

Non-goals:
- Do not fork PgBouncer itself.
- Do not introduce breaking changes.
- Do not change default auth behavior unless LDAP is explicitly enabled.

---

## Architecture constraints

Pooler CRD
→ Admission webhook (validation & defaulting)
→ Operator reconciliation
→ PgBouncer config generator
→ PgBouncer Pod

Follow existing CloudNativePG patterns and conventions.

---

## CRD design rules

Extend the Pooler CRD with an optional LDAP configuration block:

spec:
  ldap:
    enabled: boolean
    host: string
    port: integer (default 389)
    baseDN: string
    bindDN: string
    searchFilter: string (default "(uid=%u)")
    tls:
      enabled: boolean
      skipVerify: boolean
    credentials:
      secretName: string

Rules:
- spec.ldap is optional
- Backward compatibility is mandatory
- Secrets must be Kubernetes Secrets only
- No sensitive data in CRD or logs

---

## Webhook validation rules

- LDAP and auth_query are mutually exclusive
- If ldap.enabled=true:
  - host is required
  - baseDN is required
  - bindDN is required
  - credentials.secretName is required
- Apply defaults for port and searchFilter
- Reject invalid TLS configurations

---

## Operator behavior

- Generate PgBouncer LDAP configuration only when enabled
- Mount LDAP credentials Secret into PgBouncer Pod
- Never log sensitive values
- Trigger rolling restart on LDAP config changes

---

## PgBouncer configuration

- auth_type must switch to ldap only when enabled
- Preserve existing defaults
- Localize changes to PgBouncer config generator

---

## Testing & docs

- Add unit tests for CRD validation and config generation
- Add documentation under docs/pgbouncer/ldap-authentication.md
