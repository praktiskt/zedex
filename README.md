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
- `GET /native_app_signin`: anonymous sign-in, Zed receives an encrypted token
- `GET /client/users/me`: the signed-in user, with a generated session name and
  an identicon avatar served from `/avatar/:id`
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
| `--debug` | `false` | verbose logging |

### Environment

| Variable | Default | Effect |
| --- | --- | --- |
| `ZED_HOST` | `https://zed.dev` | upstream host for proxied calls |
| `ZED_API_HOST` | `https://api.zed.dev` | upstream host for the extension API |
| `BASE_URL` | `http://127.0.0.1:<port>` | public URL of this server, used for avatar URLs |

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
