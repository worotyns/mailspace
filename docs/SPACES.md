# Accounts, organizations and Spaces

| Concept | Meaning |
|---|---|
| Account | Your IMAP/SMTP identity and private provider credentials |
| Folder | An actual IMAP mailbox such as INBOX, Sent or Archive |
| Organization | Named team with an owner and members |
| Space | A private or team timeline aggregating relevant individual messages (many-to-many) |
| Tag | A scoped lightweight annotation on a message |

## Implemented now

Profile: edit display name and IANA timezone; switch appearance locally; sign out/revoke sessions. Organizations: create, rename, select active context, add existing local users and remove members. Private account access stays owner-only even between organization members. Organization changes have a metadata-only audit trail. No invitation emails are sent.

The local sandbox seeds Northstar studio and two actual test users. The pure UI demo's thematic channel filters are labeled Spaces. Automatic rules, thread sharing, tags and shared-account send-as are not claimed as implemented by that demo.

## Space contract for the next slices

A Space has a scope (private owner or organization), a name, authorized source set and versioned deterministic match rules. Initial rule inputs: sender/address domain, recipients, subject and authorized tags. Body matching is a separate opt-in decision for ordinary mail; ciphertext uses available metadata only. Client-decrypted text cannot enter backend rules. No model or semantic index is required.

Match results select authorized individual message references. Original email threads are transport context, never the Space membership unit. They never create shares, alter membership or grant send-as. Private sources remain private until the owner explicitly shares particular threads or authorizes a separate shared mailbox. Team-space viewers must pass both membership and thread/source ACL. Revocation is checked on every read, attachment, search result and future live event. Unknown rule/operator/provenance fails closed.

Rule changes need dry-run previews before apply; trace contains rule/input ids and safe metadata, not decrypted content. Classification is idempotent and does not copy or mutate remote mail. A Space is a view, not an IMAP folder or another storage server. A future move/tag/send action requires its own explicit permission and confirmation semantics. Internal reviewed contracts only; contribution remains fork → PR → review → merge.

MS-009/MS-010 cover sharing/roles/tags; MS-013 covers the optional deterministic decision engine. Their delivery must keep this distinction between relevance and permission.

## Current product direction

ADR-0016 supersedes conversation-only Space grouping: Spaces/People/labels filter one message timeline; email thread references remain intact only for original replies. Individual messages can belong to several Spaces, and one original thread can span several Spaces. Space descriptions feed optional JEV classification; an Unsorted manual workflow must work without any provider. Inline composition and a small encryption modal replace mandatory full-form replies. See ADR-0016 for the illustrative Payments/Commerce/Personal briefs and delivery order.

Storage/source foundation: ADR-0017 and migration v6 now implement private message-level Spaces/labels and explicit manual membership with current source checks. This is not yet the new public UI or a connected decision provider.
