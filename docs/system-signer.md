# System signer protocol

In the `system` signer mode, Kwakore does not hold a key. It asks a signer
service on a local Unix socket to sign for it. The service decides which key
to use from the connecting process's credentials (`SO_PEERCRED`), so each
user's Kwakore signs as that user and the key never enters Kwakore. An
operating system that signs its users in with their Nostr key can keep that
key in one privileged service and still have every user's napplets signed in
from the moment they log in.

Napplets still ask before they sign, encrypt or decrypt; the system signer
answers only after Kwakore's own permission prompt allowed the request.

## Selecting it

```json
{ "signer": { "mode": "system", "socket": "/run/nostr-signer.sock" } }
```

`socket` is optional and defaults to `/run/nostr-signer.sock`. It must be an
absolute, clean path. On NixOS:

```nix
programs.kwakore.settings.signer = { mode = "system"; socket = "/run/nostr-signer.sock"; };
```

From the command line, `kwakore signer switch system` selects the socket
named in `config.json` (or the default), and
`kwakore signer switch system --signer-socket PATH` names one. Nothing is
written to `signer-credentials.json`; a stored nsec or bunker record is kept
for a later switch back.

When the service does not answer, or answers with an error because it holds
no key for the user yet, the mode stays selected and `signer status` reports
`disconnected`. Kwakore keeps retrying in the background (after 1, 2, 5 and
10 seconds, then every 30 seconds) until the service answers or the signer is
switched again. When the identity changes, Kwakore ends the old session and
the windows bound to it, as for any other switch.

## Wire format

Each request is one connection: Kwakore connects, writes one JSON object on
one line ending in `\n`, reads one line back, and closes. The response is
either

```json
{"ok": true, "result": …}
```

or

```json
{"ok": false, "error": "human readable reason"}
```

Kwakore treats every failure the same way and never shows the reason to a
napplet. Responses longer than 1 MiB are rejected. A request is abandoned
after 90 seconds, so a service that forwards to a remote signer has time to
wait for an approval.

| `op` | Fields | `result` |
|---|---|---|
| `signer.get_public_key` | none | the user's public key, 64 lowercase hex characters |
| `signer.sign_event` | `event`: `{kind, created_at, tags, content}` | the signed event: `{id, pubkey, created_at, kind, tags, content, sig}` |
| `signer.nip44_encrypt` | `pubkey` (hex), `plaintext` | NIP-44 v2 payload (base64) |
| `signer.nip44_decrypt` | `pubkey` (hex), `ciphertext` | the plaintext |
| `signer.nip04_encrypt` | `pubkey` (hex), `plaintext` | NIP-04 payload |
| `signer.nip04_decrypt` | `pubkey` (hex), `ciphertext` | the plaintext |

Kwakore accepts a signed event only when its `pubkey` is the one
`signer.get_public_key` returned, its `kind`, `created_at`, `tags` and
`content` are exactly what it sent, its `id` matches, and its signature
verifies.

## What the service should enforce

- Pick the key from the peer's UID, never from a request field.
- Refuse peers it does not trust to have asked the user first. A service can
  compare the peer's `/proc/PID/exe` with the Kwakore daemon it expects;
  otherwise any program the user runs can sign without a prompt.
- Hold the key only while the user is signed in, and answer with an error
  while it holds none.
