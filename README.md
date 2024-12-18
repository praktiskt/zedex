# zedex

Self-hosted Zed extension API. Useful for the few who want control over what gets installed.

`zedex` can currently;
* Download the extension index
* Download individual extensions
* Serve the downloaded extension index and downloaded extensions
* (Be a transparent Zed proxy, to see what calls Zed makes)

## Usage

### Offline usage
```sh
# Download and write the index to .zedex-cache/extensions.json
zedex get extension-index

# Download all extensions to .zedex-cache/<extension id>.tar.gz
zedex get extension $(cat .zedex-cache/extensions.json | jq -r '.data[].id' | xargs)

# Serve the downloaded index and its extensions
zedex serve proxy --local-mode --port=8080
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
