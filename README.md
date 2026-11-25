# CLIProxyAPI AGY Fingerprint Plugin

**AGY Fingerprint** is an ABI v1 plugin for CLIProxyAPI that writes the official `agy` CLI User-Agent into every Antigravity credential, so upstream requests carry the same fingerprint as the real client.

Originally written by [Pokemod](https://github.com/pokemod/cpa-plugin-agy-fingerprint). This repository is a public mirror for the Plugin Store.

## Behavior

On `plugin.register` and `plugin.reconfigure` the plugin lists every credential, and for each Antigravity one sets `user_agent` and `headers.User-Agent` to the configured value. Credentials that already match are left untouched. An **AGY Fingerprint** page in the Management UI shows the last run and reapplies on demand.

```
POST /v0/management/plugins/agy-fingerprint/apply
```

## Build

```bash
make test
```

Produces `build/agy-fingerprint.so`.

## Install from the management interface

Add this registry URL to `plugins.store-sources`:

```yaml
plugins:
  enabled: true
  store-sources:
    - "https://raw.githubusercontent.com/esauvisky/CLIProxyAPI-AGY-Fingerprint-Plugin/main/registry.json"
```

## Example `config.yaml`

```yaml
plugins:
  enabled: true
  configs:
    agy-fingerprint:
      enabled: true
      # Defaults to agy 1.5.13 on linux/amd64.
      user_agent: "antigravity/cli/1.5.13 (aidev_client; os_type=linux; arch=amd64; cl=1025641825; auth_method=consumer)"
```
