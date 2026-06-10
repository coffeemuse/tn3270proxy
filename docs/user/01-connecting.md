# Connecting

Any TN3270 emulator works: c3270/x3270/wc3270, IBM PCOMM, Vista tn3270, a
web-based 3270 client, or a terminal built into your tooling. Your
administrator gives you the gateway's host name and port, and whether the
connection is **TLS** (encrypted) or plaintext — they may be different ports.

With the x3270 family:

    c3270 gateway.example.com:2323        # plaintext
    c3270 L:gateway.example.com:2324      # TLS (the L: prefix)

Other emulators have an equivalent "secure connection" checkbox or `SSL/TLS`
setting. If the gateway uses a self-signed certificate, your emulator will
warn you the first time — your administrator will tell you whether that's
expected at your site (c3270 needs `-noverifycert` to accept one).

When the connection succeeds you'll see the [login screen](02-logging-in.md)
immediately. If nothing appears or the connection drops at once, see
[Troubleshooting](09-troubleshooting.md).
