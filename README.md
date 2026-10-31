# zedex

Self-hosted Zed server. Serves Zed's HTTP API and its collaboration RPC protocol
so a client can talk to `zedex` instead of `zed.dev`.

## Capabilities

**Extensions**
- `GET /extensions`: index, filtered by `max_schema_version`, `filter`, `provides`
- `GET /extensions/updates`: updates for a set of extension ids
- `GET /extensions/:id`: metadata for one extension
- `GET /extensions/:id/download`, `GET /extensions/:id/:version/download`: archive

**Releases**
- `GET /releases/stable/latest/asset`: latest version and download URL
- `GET /api/releases/latest`: same, for the `api` path
- `GET /api/release_notes/v2/stable/:version`: release notes

**Accounts**
- `GET /native_app_signin`: the sign in page, Zed receives an encrypted token
- `GET /oauth/start`, `GET /oauth/callback`: the OAuth flow
- `GET /oauth/guest`: an anonymous session
- `GET /client/users/me`: the signed-in user, with a name and an avatar
- `POST /client/llm_tokens`: LLM token
- `GET /client/users/connect`: websocket for cloud notifications
- `GET /account`: account page
- `GET /client/system_settings`, `PATCH /client/system_settings`

**Collaboration (RPC over websocket at `/handle-rpc`)**
- Channels: create, join, rename, delete, visibility, reorder, invite members,
  accept or decline invites, member roles, channel messages
- Channel notes: a CRDT buffer per channel, with operation history, replica ids
  and version vectors, so clients joining later receive the history
- Shared projects: share a project into a room, other participants join it and
  exchange file buffer operations with the host
- Rooms (calls): create, join, leave, participant locations
- Contacts: request, accept, decline, remove
- Notifications: list, add, update, delete, mark read
- Ping and keepalive

**Proxy**
- `GET /api/*` is proxied to `ZED_HOST`
- Extensions and releases can be proxied instead of served from disk

## Usage

```sh
# Extension index into .zedex-cache/extensions.json
zedex get extension-index

# All extension archives into .zedex-cache/<extension id>.tar.gz
jq -r '.data[].id' .zedex-cache/extensions.json | xargs zedex get extension

# Latest release and its notes
zedex get latest-release

# Serve
zedex serve --port=8080
```

Point Zed at it:

```json
{
  "server_url": "http://localhost:8080"
}
```

### Flags

| Flag | Default | Effect |
| --- | --- | --- |
| `--port` | `8080` | listen port |
| `--output-dir` | `.zedex-cache` | where the extension index and archives live |
| `--enable-extension-store` | `true` | serve extensions from disk, `false` proxies to Zed |
| `--enable-releases` | `true` | serve the latest release from disk, `false` proxies to Zed |
| `--enable-release-notes` | `true` | serve release notes from disk, `false` proxies to Zed |
| `--enable-login` | `true` | handle sign-in requests |
| `--auth-config` | none | path of the authentication configuration |
| `--debug` | `false` | verbose logging |

### Environment

| Variable | Default | Effect |
| --- | --- | --- |
| `ZED_HOST` | `https://zed.dev` | upstream host for proxied calls |
| `ZED_API_HOST` | `https://api.zed.dev` | upstream host for the extension API |
| `BASE_URL` | `http://127.0.0.1:<port>` | public URL of this server, used for avatar URLs |
| `ZEDEX_AUTH_CONFIG` | none | path of the authentication configuration |

## Authentication

Without a configuration `zedex` signs everyone in anonymously: every visitor
gets a random identity with a generated name and an identicon avatar. That is
the behaviour of a server that does not care who is on it.

With a configuration `zedex` signs users in with an OAuth provider, so that
the same person keeps the same identity, the same name and the same avatar
across restarts. The anonymous option stays on the sign in page next to the
providers.

### Configuration

```json
{
  "public_url": "https://zed.example.com",
  "identity_secret": "a-secret",
  "session_secret": "another-secret",
  "token_ttl_seconds": 0,
  "allow_guest": true,
  "providers": [
    {
      "name": "github",
      "type": "github",
      "client_id": "env:GITHUB_CLIENT_ID",
      "client_secret": "env:GITHUB_CLIENT_SECRET",
      "primary": true
    },
    {
      "name": "okta",
      "type": "oidc",
      "issuer": "https://example.okta.com/oauth2/default",
      "client_id": "env:OKTA_CLIENT_ID",
      "client_secret": "env:OKTA_CLIENT_SECRET"
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `public_url` | the address of the server, the OAuth redirect URI is derived from it |
| `identity_secret` | derives the numeric user id from an identity, changing it changes every user id |
| `session_secret` | signs the access tokens, changing it signs everyone out but keeps the user ids |
| `token_ttl_seconds` | how long an access token stays valid, `0` means forever |
| `allow_guest` | whether users may sign in without an account, `true` by default |
| `providers` | the OAuth providers users can sign in with |

A secret can be given as `env:NAME` to read it from the environment. Generate
the two secrets with:

```sh
zedex auth secret
```

A provider is either `github` or `oidc`. The GitHub provider asks for the
`read:user` and `user:email` scopes. The OpenID Connect provider finds its
endpoints through the issuer's well known configuration, and
`authorization_endpoint`, `token_endpoint` and `userinfo_endpoint` override
that.

The logins of the `primary` provider are used as usernames as they are. The
logins of every other provider get the name of the provider appended, so that
usernames stay unique.

### The flow

1. Zed opens `/native_app_signin` in the browser.
2. The page offers the providers and, when it is allowed, an anonymous
   session.
3. The browser goes to the provider, which sends the user back to
   `/oauth/callback`.
4. `zedex` exchanges the code, derives the user id, and sends the browser back
   to Zed with the user id and an access token that is encrypted with the key
   Zed generated.
5. Zed sends the token in the `Authorization` header of every request, as
   `<user id> <access token>`. `zedex` checks it.

The user id is derived from the identity the provider reports, so it is the
same on every restart and on every server that uses the same
`identity_secret`. The access token carries the profile of the user and is
signed, so `zedex` does not have to keep any state.

Register `{public_url}/oauth/callback` as the redirect URI of the OAuth
application.

## Edit prediction

Not served by `zedex`. Zed supports custom edit-prediction providers
(including OpenAI-compatible APIs) in its own settings, so configure them there.
`zedex` answers the edit-prediction endpoints with `501` so the client does not
retry in a loop.

## Not implemented

Voice and video calls (LiveKit), agent threads, Delta, and the LSP and git RPC
surfaces Zed uses for shared projects.

## Building

```sh
make build
```
