# Trusted Networks

Admin menu option **5** — an allow-list of source networks exempt from the
gateway's pre-authentication DoS protections. Stored in the database and
applied to **new connections immediately**, no restart.

## What trusting a network changes

A connection from a trusted network skips:

- the **pre-auth idle timeout** and the **pre-auth absolute ceiling** — a
  trusted client can sit at the login screen indefinitely (useful for
  consoles or wall-mounted terminals that stay parked there), and
- the **per-IP connection cap** — handy for web-based 3270 clients that
  funnel many users through one gateway IP.

A trusted connection still counts toward the **global** connection cap, and
authentication, MFA, and auth throttling apply unchanged — trust relaxes
resource limits, never security checks. (Trusted sources are also marked
`trusted=true` in auth-failure log lines so external ban scanners can spare
them — see [Logging](13-logging.md).)

## Managing entries

Line commands: **S** = edit, **D** = delete (confirm-gated). **PF4** adds an
entry. Each entry is:

| Field | Rules |
|---|---|
| Network | an IP (`192.168.1.5`) or CIDR (`10.0.0.0/24`); bare IPs become host routes (/32, /128); stored in canonical masked form |
| Comment | required, max 40 — say *why* it's trusted |

Deleting an entry affects new connections only; already-connected trusted
clients keep their exemptions for the life of the connection.

## Use sparingly

Every trusted network is a hole in the DoS posture. Trust only networks you
control, prefer narrow CIDRs over broad ones, and use the required comment
to record the reason so future admins can re-evaluate.
