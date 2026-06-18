# Managing services

Admin menu option **3**. The list shows each backend service: name,
description, host:port, and its TLS settings.

Line commands: **S** = edit, **G** = manage which groups can see it, **D** =
delete (confirm-gated). **PF4** adds a service.

## The service form

| Field | Rules |
|---|---|
| Name | required; A–Z and 0–9 only, max 8; stored uppercase; unique |
| Description | required, max 40 characters — this is the label users see on the menu |
| Host | required; **not** case-folded |
| Port | 1–65535 |
| TLS (Y/N) | `Y` = the gateway dials the backend over TLS |
| Verify (Y/N) | only meaningful with TLS = Y; default `Y` |

The backend host and port are never shown to end users — the menu displays
only the name and description.

## Backend TLS and certificate verification

- **TLS = Y, Verify = Y** (the secure default): the backend's certificate is
  verified like a browser would — against the system trust roots, with
  hostname checking. The expected name is always the configured **Host**
  value, so if you connect by IP address, the backend's certificate needs an
  **IP SAN**; a hostname-only certificate will fail verification.
- **TLS = Y, Verify = N**: encrypts the connection but accepts any
  certificate. Use for internal hosts with self-signed certificates —
  understand you get confidentiality without authentication.
- **TLS = N**: plaintext to the backend.

## Group access (`G`)

Shows every group with an `X` next to those granted access. **A** = grant,
**R** = revoke. A service granted to no groups still exists — it's just on
nobody's menu. Access changes appear at the user's next menu render or
login; an already-bridged session is not interrupted.

## Deleting a service

`D`, then Enter to confirm. Active bridges to that backend are not cut —
users already connected stay connected until they leave or are
[disconnected](11-sessions.md).
