# OAuth

This document follows the writing rules of ASD-STE100.

## Scope

This document describes how zedex authenticates users with an OAuth provider.

zedex supports two modes of authentication. Without a configuration file, zedex
gives each visitor an anonymous identity. With a configuration file, zedex gives
each visitor an identity that comes from an OAuth provider.

The anonymous mode stays available in both cases. A visitor can select the
anonymous mode. The visitor does not need an account.

zedex keeps no user database. The user id comes from a hash of the identity
that the provider reports. The access token carries the profile of the user.
Because of this, zedex does not need to store any user data.

## The browser flow

Zed does not ask for a user name and a password. Zed opens a page in a browser
and waits for a callback on a local port. The page is the only place where a
user can choose an account. Therefore, zedex serves the page itself and
controls the whole flow.

The flow uses the authorization code grant. The flow also uses PKCE. The
provider never sees the credentials of the user. The provider sees only the
public data of the client.

### The sequence

| Step | Action | Data |
| --- | --- | --- |
| 1 | Zed opens the login page. | The port and the public key of Zed. |
| 2 | zedex saves the pending sign in. | The port, the public key, and the redirect URI. |
| 3 | zedex returns the login page. | The list of the providers. |
| 4 | The user selects a provider. | The name of the provider. |
| 5 | zedex returns a redirect to the provider. | The state and the PKCE challenge. |
| 6 | The provider sends the browser to the callback. | The code and the state. |
| 7 | zedex exchanges the code for a token. | The profile of the user. |
| 8 | zedex calculates the user id. | The user id. |
| 9 | zedex creates an access token. | The access token. |
| 10 | zedex sends the browser to Zed. | The user id and the encrypted token. |

### The pending sign in

The state of step 5 is the identifier of the pending sign in. The identifier
is a random string of 32 bytes. The pending sign in holds the port, the public
key, the redirect URI, the name of the provider, and the PKCE verifier.

A server keeps a pending sign in for 10 minutes. A client can complete a
pending sign in only once. A client can start a pending sign in only once.

## Configuration

The configuration file is a JSON file. The path of the file comes from the
`--auth-config` flag. The path of the file also comes from the
`ZEDEX_AUTH_CONFIG` environment variable. The file `zedex-auth.example.json`
shows an example with two providers.

| Field | Meaning |
| --- | --- |
| `public_url` | The address of the server. The redirect URI comes from this address. |
| `identity_secret` | The key that calculates the user id. A new value changes every user id. |
| `session_secret` | The key that signs the access token. A new value signs out every user. The user ids do not change. |
| `token_ttl_seconds` | The lifetime of the access token in seconds. The value 0 means that the token does not expire. |
| `allow_guest` | Whether a visitor can use the anonymous mode. The default is true. |
| `providers` | The list of the OAuth providers. |

A provider has the following fields.

| Field | Meaning |
| --- | --- |
| `name` | The name of the provider. The name must be unique. |
| `type` | The type of the provider. The value is `github` or `oidc`. |
| `display_name` | The label of the button on the login page. |
| `client_id` | The client id of the OAuth application. |
| `client_secret` | The client secret of the OAuth application. |
| `scopes` | The scopes that the provider requests. |
| `issuer` | The issuer URL of an OpenID Connect provider. |
| `authorization_endpoint` | The authorization endpoint. This field overrides the discovery. |
| `token_endpoint` | The token endpoint. This field overrides the discovery. |
| `userinfo_endpoint` | The userinfo endpoint. This field overrides the discovery. |
| `primary` | Whether the logins of the provider are used as usernames. |

A secret can have the form `env:NAME`. zedex then reads the value of the
secret from the environment variable `NAME`. The command `zedex auth secret`
generates a secret.

The logins of the primary provider are the usernames. The logins of the other
providers get the name of the provider. For example, the login `magnus` of the
provider `okta` becomes the username `magnus@okta`. This rule keeps the
usernames unique.

## The user id

Zed knows a user by a number. The number is the legacy user id. The number
must fit into a signed 32-bit integer.

zedex calculates the user id in this way:

```
user_id = HMAC-SHA256(identity_secret, provider + "\x00" + subject)
```

zedex takes the first four bytes of the hash. zedex masks the value with
`0x7fffffff`. The result is a number from 1 to 2147483647.

The `subject` is the identifier that the provider gives to the user. For
GitHub, the subject is the numeric id of the account. For OpenID Connect, the
subject is the `sub` claim of the token.

The same identity always gives the same user id. Two servers that use the same
`identity_secret` give the same user id to the same person. A new
`identity_secret` gives new user ids to all people.

An anonymous session uses a random subject. Therefore, an anonymous session
gets a new user id for every sign in.

## The access token

The access token proves the identity of the client. Zed sends the token in the
`Authorization` header of every request. The header has this form:

```
Authorization: <user id> <access token>
```

The token is a string of three parts. A dot separates the parts.

```
v1.<payload>.<signature>
```

The payload holds the user id, the provider, the subject, the username, the
name, the email address, and the avatar URL. The payload is a binary structure.
The signature is the first 16 bytes of the HMAC-SHA256 of the payload. The
key of the HMAC is the `session_secret`.

The server verifies the signature. The server then compares the user id of the
header with the user id of the payload. The server rejects the request if the
values differ.

### The size limit

Zed generates a 2048-bit key pair when the user signs in. zedex encrypts the
access token with the public key of Zed. The encryption uses OAEP with a
SHA-256 digest. The largest plaintext of such a key is 190 bytes.

The token must therefore fit into 190 bytes. This limit includes the
signature. zedex limits the length of each field of the payload. When the
payload is still too large, zedex removes the fields in this order:

1. The email address.
2. The avatar URL.
3. The name.
4. The subject.

The user id, the provider, and the username always remain in the token.

### The lifetime

The `token_ttl_seconds` field sets the lifetime of the token. The default
value of 0 means that the token does not expire. A token with a lifetime
expires at the given time. Zed checks the lifetime at every request.

A new `session_secret` invalidates every token. This is the only way to sign
out all users without a restart.

## The endpoints

| Endpoint | Purpose |
| --- | --- |
| `GET /native_app_signin` | The login page. The request carries the port and the public key of Zed. |
| `GET /oauth/start` | The start of a provider flow. The request carries the identifier of the pending sign in and the name of the provider. |
| `GET /oauth/callback` | The return of the provider. The request carries the code and the state. |
| `GET /oauth/guest` | The anonymous mode. The request carries the identifier of the pending sign in. |
| `GET /client/users/me` | The identity of the caller. The server checks the access token. |
| `GET /client/users/connect` | The cloud notification socket. The server checks the access token. |
| `GET /handle-rpc` | The collaboration socket. The server checks the access token. |

## The verification

zedex checks the access token at three places. These places are the cloud
identity endpoint, the cloud notification socket, and the collaboration socket.

A request with a missing token gets the status 401. A request with a token that
the server did not create gets the status 401. A request with a token of
another user gets the status 401.

A server without a configuration file does not check the access token. Such a
server uses the user id of the header. Such a server gives the user a random
name.

## The limits

Zed waits 100 seconds for the callback of the sign in. A slow interaction with
the provider can exceed this time. Zed then reports an error. This limit comes
from Zed. A server cannot change it.

The avatar URL is usually too long for the token. Therefore, only the server
that handled the sign in knows the avatar of a user. After a restart, the
server shows the identicon of the user until the user signs in again. The user
id and the username remain the same.

A server does not know the profile of a user that never signed in. Such a user
gets a generated name.

## The code

| File | Purpose |
| --- | --- |
| `src/zed/auth/config.go` | The configuration file and the validation. |
| `src/zed/auth/provider.go` | The provider interface and the HTTP helpers. |
| `src/zed/auth/github.go` | The GitHub provider. |
| `src/zed/auth/oidc.go` | The OpenID Connect provider and the discovery. |
| `src/zed/auth/token.go` | The token format, the signature, and the size limit. |
| `src/zed/auth/manager.go` | The user id, the token, and the profile cache. |
| `src/zed/auth/flow.go` | The pending sign in and the PKCE verifier. |
| `src/zed/signin.go` | The HTTP handlers of the flow. |
