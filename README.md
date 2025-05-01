# zedex

Self-hosted Zed server.

`zedex` can currently;
* Download the extension index
* Download individual extensions
* Download the latest release, and its release notes
* Serve the downloaded extension index and downloaded extensions
* List the latest version of Zed, and store a reference to it (version+url), and its release notes
* Log in anonymously.
* Use any OpenAI-compatible backend for edit prediction
  * Note: It works, kind of, but needs more work.
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
zedex serve --port=8080

# You can run certain features in "passthrough" mode by disabling them in zedex. The
# following command will fetch extensions and releases from zed, but handle the rest
# of the calls itself.
zedex serve --enable-extension-store=false --enable-releases=false
```

Modify the Zed-settings file (`settings.json`) to use the proxy:
```json
{
  "server_url": "http://localhost:8080"
}
```

### Configuring an OpenAI-compatible backend
In order to use this, you must be logged into Zed (edit prediction is login-gated by the Zed client). `zedex` supports an anonymous login which does the trick.

Running `zedex` with `--enable-edit-prediction` (default) allows you to configure an OpenAI compatible backend to manage your requests. Set the following environment variables if you wish;
```sh
# If you do not want to use edit prediction, you can disable it.
# export OPENAI_COMPATIBLE_DISABLE=true

# Configure a server
export OPENAI_COMPATIBLE_HOST="https://some.url.here/v1/chat/completions"
export OPENAI_COMPATIBLE_API_KEY="add your key here"
export OPENAI_COMPATIBLE_MODEL="name of the model to use"

# Optionally, you can modify the system prompt. Note that this needs to keep some special tokens intact that Zed ships in its requests. You'll probably need to dig through the zedex source to find how we do it at the moment.
# export OPENAI_COMPATIBLE_SYSTEM_PROMPT="You are a code autocomplete engine."
```

The default for zedex is to use [Groq](https://groq.com/).

## Building

```sh
make build
```
