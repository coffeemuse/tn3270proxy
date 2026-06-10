# Security Policy

TN3270Proxy is an authentication gateway designed to sit at the edge of a network,
so security reports are taken seriously. Thank you for helping keep it and its
users safe.

## Supported versions

This project is pre-1.0; only the **latest tagged release** receives security
fixes. Once 1.0 ships, the latest **1.x** release will be supported.

| Version          | Supported |
| ---------------- | --------- |
| Latest release   | ✅        |
| Older releases   | ❌        |

## Reporting a vulnerability

**Please do not report security issues in public GitHub issues, pull requests, or
discussions.** Report privately using:

1. **GitHub Security Advisories (preferred)** — use the repository's
   **Security → Report a vulnerability** form:
   <https://github.com/coffeemuse/TN3270Proxy/security/advisories/new>. This keeps
   the report private until a fix is published.


Please include, as far as you can:

- the version (`tn3270proxy version`) and how it's deployed (Docker / binary / source),
- a description of the issue and its impact,
- steps to reproduce or a proof of concept,
- any suggested remediation.

**Do not include live credentials, TOTP secrets, or the MFA master key** in your report.

## What to expect

This is a personal, best-effort project — there is no paid security team and no
formal SLA. That said, the maintainer aims to:

- acknowledge a report within **7 days**,
- give an initial assessment within **30 days**,
- coordinate a fix and a disclosure timeline with you, and credit you (if you wish)
  in the release notes.

## Scope

**In scope:** the proxy itself — authentication, MFA, session handling, the admin
UI, the bridge, audit logging — and the release artifacts (binaries and the
container image).

**Out of scope:** vulnerabilities in the backend TN3270 hosts you bridge to;
third-party dependencies (report those upstream — we track them with `govulncheck`
and Dependabot); and issues that require an already-compromised host or physical
access.
