# zedex

Self-hosted Zed extension API. Useful for the few who want control over what gets installed.

`zedex` can currently;
* Download the extension index
* Download individual extensions
* Serve the downloaded extension index and downloaded extensions
* List the latest version of Zed, and store a reference to it (version+url)
* (Be a transparent Zed proxy, to see what calls Zed makes)

## Usage

### Offline usage
```sh
# Download and write the index to .zedex-cache/extensions.json
zedex get extension-index

# Download all extensions to .zedex-cache/<extension id>.tar.gz
zedex get extension $(cat .zedex-cache/extensions.json | jq -r '.data[].id' | xargs)

# Download info about the latest release to .zedex-cache/latest_release.json
zedex get latest-release

# Serve the downloaded index, its extensions and info about the latest release
zedex serve --local-mode --port=8080
```

Modify the Zed-settings file (`settings.json`) to use the proxy:
```json
{
  "server_url": "http://localhost:8080"
}
```

## Building

```sh
make build
```
