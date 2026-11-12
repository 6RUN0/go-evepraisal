# Evepraisal
Evepraisal is a bulk-price estimator for Eve Online.

## Docker Instructions (production)

Requirements: Docker with the compose plugin.

1. Put `Dockerfile`, `docker-compose.yml` and `evepraisal.toml` into one
directory.
2. Create the data directory and give it to the container user (UID 65532):

   ```sh
   mkdir data && sudo chown 65532:65532 data
   ```

3. Start the service:

   ```sh
   docker compose up -d --build
   ```

Layout inside the container:

- `/data` is the only writable path (bind-mounted from `./data`). Databases
  live in `/data/db/`, backups in `/data/db/backups/`.
- `/etc/evepraisal/evepraisal.toml` is the config file, mounted read-only.
- The root filesystem is read-only and the process runs as UID 65532.

Restore appraisals from a backup file placed under `./data`, with the same
user and read-only root filesystem:

```sh
docker compose run --rm evepraisal restore -files /data/db/backups/appraisals.gz
```

### Reverse proxy

Behind Angie or nginx, pass the client address and disable response buffering
(needed for server-sent events):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_buffering off;
}
```

## Configuration

Keys are read from `evepraisal.toml` (the working directory or
`/etc/evepraisal/`). Each key can also be set through the environment:
upper-case it, prefix `EVEPRAISAL_`, keep the characters as they are, for
example `base-url` becomes `EVEPRAISAL_BASE-URL`. The environment overrides
the file.

| Key | Default | Purpose |
| --- | --- | --- |
| `base-url` | `http://127.0.0.1:8080` | Public URL of the site; used for links and the SSO callback |
| `http_addr` | `:8080` | Listen address of the HTTP server |
| `http_redirect` | `false` | Redirect HTTP to HTTPS |
| `https_addr` | empty | Listen address of the HTTPS server; empty disables HTTPS |
| `https_domain-whitelist` | `["evepraisal.com"]` | Domains for which Let's Encrypt certificates are requested |
| `letsencrypt_email` | empty | Contact e-mail for Let's Encrypt |
| `db_path` | `db/` (`/data/db/` in the image) | Directory of the databases |
| `backup_path` | `db/backups/` (`/data/db/backups/` in the image) | Directory of appraisal backups |
| `esi_baseurl` | `https://esi.evetech.net/latest` | ESI base URL |
| `newrelic_app-name` | `Evepraisal` | New Relic application name |
| `newrelic_license-key` | empty | New Relic license key; empty disables New Relic |
| `management_addr` | `127.0.0.1:8090` | Listen address of the management server |
| `site-name` | `Evepraisal` | Site name in the navbar and page titles |
| `source-url` | `https://github.com/evepraisal/go-evepraisal` | Source repository; the footer links to it and to its `/issues` |
| `about-contacts-html` | the four upstream contact items | HTML of the contact list on the about page, inserted unescaped |
| `favicon-path` | empty | Icon file served at `/favicon.ico`; empty serves the built-in icon; an unreadable file stops the start |
| `extra-html-header` | empty | HTML added to the page head |
| `extra-js` | empty | JavaScript added to every page |
| `ad-block` | empty | HTML of the ad block |
| `extra-static-file-path` | empty | Directory of extra static files, served at their path relative to it |
| `cookie-auth-key` | none | Session cookie signing key; random on every start if unset |
| `cookie-encryption-key` | none | Session cookie encryption key |
| `sso-client-id` | none | EVE SSO client ID; empty disables login |
| `sso-client-secret` | none | EVE SSO client secret |
| `sso-authorize-url` | `https://login.eveonline.com/oauth/authorize` | SSO authorize endpoint |
| `sso-token-url` | `https://login.eveonline.com/oauth/token` | SSO token endpoint |
| `sso-verify-url` | `https://login.eveonline.com/oauth/verify` | SSO verify endpoint |

## Instructions (development)
The following was tested on Ubuntu Server 18.10
- Install golang 1.11
```
  ~$ curl https://dl.google.com/go/go1.11.10.linux-amd64.tar.gz | tar xz
  ~$ sudo mv go /usr/local
  ~$ echo 'export GOROOT=/usr/local/go' >>~/.profile
  ~$ echo 'export GOPATH=$HOME/go' >>~/.profile
  ~$ echo 'export PATH=$GOPATH/bin:$GOROOT/bin:$PATH' >>~/.profile
  ~$ echo 'export GO111MODULE=on' >>~/.profile
  ~$ source ~/.profile
```
- Install build requirements
```
  ~$ sudo apt install git gcc musl-dev make
```
- Download and build evepraisal
```
  ~$ mkdir -p $GOPATH/src/github.com/evepraisal/go-evepraisal
  ~$ cd $GOPATH/src/github.com/evepraisal/go-evepraisal
  ~/go/src/github.com/evepraisal/go-evepraisal$ git clone https://github.com/evepraisal/go-evepraisal.git .
  ~/go/src/github.com/evepraisal/go-evepraisal$ make setup
  ~/go/src/github.com/evepraisal/go-evepraisal$ make build
```
- Run evepraisal
```
  ~/go/src/github.com/evepraisal/go-evepraisal$ ./target/evepraisal-linux-amd64
```
