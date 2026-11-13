# Synthetic encrypted MIME fixture

`encrypted.eml` contains only a generated OpenPGP message for fixture@example.test,
with an embedded signature and wildcard recipient IDs. Its ephemeral private key
was discarded, not checked into the repository. The literal MIME text was
"Synthetic encrypted fixture, no real mail." Backend tests validate opaque
transport/ACL/outbox behavior; cryptographic interoperability is covered by the
browser-module tests with an isolated GnuPG home. This file is AGPL-3.0-only test
data, not a real mailbox export.
