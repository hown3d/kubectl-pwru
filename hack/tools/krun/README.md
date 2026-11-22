# Krun runtime

## Install to your host

`bash ./copy-to-host.sh`

## setup docker engine

Add to your docker daemon.json:

```json
"runtimes": {
  "krun": {
    "path": "/usr/local/bin/krun"
  }
}
```
