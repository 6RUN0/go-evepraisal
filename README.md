# Evepraisal

Evepraisal is a bulk-price estimator for EVE Online: paste a cargo scan,
contract, fitting, inventory listing and similar text, get the market value of
the items. This repository is a fork of
[evepraisal/go-evepraisal](https://github.com/evepraisal/go-evepraisal); it
loads item types from CCP's JSONL static data export (SDE), reads market
orders from the dated ESI API and ships as a non-root container image.
Changes against upstream are listed in [CHANGELOG.md](CHANGELOG.md).

This file covers running the service (Docker), its configuration, data and
operations, then building from source.

## Install with Docker

Requirements: Docker with the compose plugin and outbound HTTPS to these
hosts:

| Host | Needed for |
| --- | --- |
| `esi.evetech.net` | market orders and CCP prices, always |
| `developers.eveonline.com` | SDE manifest and archive, always |
| `login.eveonline.com` | token exchange and verification, with `sso-client-id` |
| `acme-v02.api.letsencrypt.org` | certificates, with `https_addr` |
| New Relic collector hosts | metrics, with `newrelic_license-key` |

Upgrading an installation made with the upstream compose file: follow
[Migrate from the upstream compose file](#migrate-from-the-upstream-compose-file)
instead.

1. Clone the repository and run every command below from its root: the image
   is built from the full source tree, and `docker-compose.yml` and
   `evepraisal.toml` live there.

   ```sh
   git clone https://github.com/6RUN0/go-evepraisal.git evepraisal
   cd evepraisal
   ```

2. Set `base-url` in `evepraisal.toml` to the public URL of the site (see
   [Configuration](#configuration)).

3. Create the data directory and give it to the container user (UID 65532).
   The service creates `db/` and `db/backups/` inside it on start:

   ```sh
   mkdir data && sudo chown 65532:65532 data
   ```

4. Start the service:

   ```sh
   docker compose up -d --build
   ```

5. Wait until the container is healthy:

   ```sh
   docker compose ps
   ```

   The `STATUS` column reads `Up ... (healthy)`. On an empty data directory
   this takes minutes, see [First start](#first-start-on-an-empty-data-directory).

### Published image

Instead of building in step 4, the service can run the image CI publishes to
`ghcr.io/6run0/go-evepraisal` (`linux/amd64` only). The clone is still needed
for `docker-compose.yml` and `evepraisal.toml`. In `docker-compose.yml`
replace `build: .` with the image and a tag from the table:

```yaml
services:
  evepraisal:
    image: ghcr.io/6run0/go-evepraisal:1.2.3
```

Then start without `--build`:

```sh
docker compose up -d
```

| Tag | Points to | Changes |
| --- | --- | --- |
| `X.Y.Z` | release tag `vX.Y.Z` | never |
| `X.Y` | most recently pushed `vX.Y.*` tag | on each patch release |
| `latest` | highest released version | when a higher version is released |
| `edge` | newest commit on `master`, not a release | on every push to `master` |
| `sha-<commit>` | that `master` commit (7-character short SHA) | never |

An image is pushed only after `make check` passes on that commit. A release
is a tag of the form `vX.Y.Z` (digits only, no suffix) on a commit of
`master`: other tags start no build, and a matching tag on a commit outside
`master` fails the build before anything is pushed. Releasing a patch of an
older line (`v1.2.5` after `v1.3.0`) moves `1.2` but not `latest`. Who may
push or move `v*` tags is set by a tag ruleset in the repository settings on
GitHub (Settings, Rules, Rulesets); without one, anyone with write access can
release.

`edge` carries unreleased commits; pin an `X.Y.Z` tag in production (`1.2.3`
above is an example).

Verify that an image was built by this repository's CI from the source it
names (needs the GitHub CLI):

```sh
gh attestation verify oci://ghcr.io/6run0/go-evepraisal:1.2.3 -R 6RUN0/go-evepraisal
```

The command exits with status 0 and prints the workflow and commit that
built the image; a non-zero exit means the image did not come from this
repository's CI or its attestation is missing. The image also carries BuildKit provenance and an
SBOM:

```sh
docker buildx imagetools inspect ghcr.io/6run0/go-evepraisal:1.2.3 --format '{{ json .SBOM }}'
```

### Logs

The service writes its log to stderr, one line per event:

```sh
docker compose logs -f evepraisal
```

Docker keeps the log with its configured logging driver; the service itself
writes no log files and does no rotation.

### Container layout

| Path | Mode | Content |
| --- | --- | --- |
| `/data` | writable, bind mount of `./data` | databases in `/data/db/`, backups in `/data/db/backups/` |
| `/etc/evepraisal/evepraisal.toml` | read-only, bind mount of `./evepraisal.toml` | config file |
| `/tmp` | tmpfs, 16 MB | scratch space |
| everything else | read-only root filesystem | distroless static image, no shell, no curl |

The process runs as UID 65532 with all capabilities dropped, so it cannot
bind ports below 1024 inside the container. Compose publishes port 8080 on
all host interfaces; to reach it only through a reverse proxy, change `ports`
to `["127.0.0.1:8080:8080"]`.

The image sets `EVEPRAISAL_DB_PATH=/data/db/` and
`EVEPRAISAL_BACKUP_PATH=/data/db/backups/`. The environment overrides the
config file, so `db_path` and `backup_path` in the mounted `evepraisal.toml`
have no effect inside the container.

The appraisal database maps its file size plus 2 GB of virtual memory at
start (`bolt/appraisal_db.go`). Resident memory is lower, but a limit on
virtual memory (`ulimit -v`, `vm.overcommit_memory = 2`) can stop the start
(read from the code, not measured).

### Reverse proxy

Behind Angie or nginx pass the `Host` header and the client address (the
access log takes it from `X-Real-Ip`, then `X-Forwarded-For`, then the TCP
peer):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-Ip $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
}
```

When the proxy terminates TLS, keep `http_redirect = false`: the service
cannot tell a proxied HTTPS request from a plain one and would redirect every
request to `https://` again, an endless loop.

### HTTPS without a proxy

The service can terminate TLS itself with Let's Encrypt certificates
(HTTP-01 challenge). Preconditions, none of which the default compose file
meets:

- `https_addr` set to a port above 1024, for example `:8443`;
- the host's port 443 published to it and port 80 published to `http_addr`
  (`ports: ["80:8080", "443:8443"]`), both reachable from the internet: the
  challenge is answered only on the HTTP port;
- `https_domain-whitelist` set to the site's domain names, and DNS pointing
  to the host.

Certificates are stored in `<db_path>/certs/`. A failed issuance shows up
only as a TLS handshake error in the browser and in the service log; the
service keeps running.

`http_redirect = true` replaces the challenge handler on the HTTP port with
the redirect (read from the code). Whether issuance still succeeds through
that redirect was not tested: get the first certificate with
`http_redirect = false`.

## Configuration

Keys are read from `evepraisal.toml`, searched in `/etc/evepraisal/` first,
then in the working directory. A missing file is not an error: the service
logs `No config file found, using defaults searched_paths=["/etc/evepraisal/" "."]`.
Any other problem with the file (a syntax error, no read permission) stops
the start with `Invalid config file: <path>: <reason>`.

On start the service logs every key that has a default or is set in the
file or the environment, in key order; keys of a TOML table appear as
`<table>.<key>`. The values of `cookie-auth-key`, `cookie-encryption-key`,
`sso-client-secret`, `newrelic_license-key` and of every key whose name
contains `key`, `secret`, `password` or `token` (any case) are printed as
`<set>` or `<unset>`, never in clear. The four named keys are listed even
when unset.

### Environment variables

Each key can also be set through the environment. The variable name is the
key upper-cased with `-` replaced by `_` and prefixed with `EVEPRAISAL_`:
`base-url` becomes `EVEPRAISAL_BASE_URL`, `db_path` becomes
`EVEPRAISAL_DB_PATH`. The table below lists every name.

- **Legacy names.** The key upper-cased with `-` kept
  (`EVEPRAISAL_BASE-URL`) is still read. If both names are set to different
  values the start stops with `Invalid environment: EVEPRAISAL_BASE_URL and
  EVEPRAISAL_BASE-URL are both set with different values; set only
  EVEPRAISAL_BASE_URL`; the same value under both names is accepted.
- **Files.** `<variable>_FILE` (`EVEPRAISAL_COOKIE_AUTH_KEY_FILE`) names a
  file whose content is the value, for Docker or compose secrets. The file is
  read once at start; exactly one trailing line ending (`\n` or `\r\n`) is
  removed, nothing else. An empty file sets a text key to an empty string;
  for `db_path`, `backup_path`, `market-fetch-interval` and `http_redirect`
  it stops the start with `Invalid environment: <key>: <variable>_FILE names
  an empty file`. Only the portable name takes `_FILE`. A variable and its
  `_FILE` both set stops the start with `Invalid environment: <variable> and
  <variable>_FILE are both set; set only one`. A missing or unreadable file,
  or a directory, stops it with `Invalid environment: <key>: read
  <variable>_FILE: <reason>`, where the reason names the path; the content
  is never printed.
- **Unknown variables.** A non-empty `EVEPRAISAL_*` variable that is no
  accepted name of any key (a typo, or a legacy name with `_FILE` such as
  `EVEPRAISAL_BASE-URL_FILE`) is ignored and logged once at start as
  `WARN: Unknown environment variable ignored name=<variable>`, without its
  value.
- **Precedence.** A variable or its `_FILE` overrides the config file, the
  config file overrides the default. An empty variable (`EVEPRAISAL_X=` or
  `EVEPRAISAL_X_FILE=`) counts as unset: the file or the default applies.
- **Types.** Values parse as in the file: `true`/`false` for
  `http_redirect`, a whole number for `market-fetch-interval`, and a
  space-separated list for `https_domain-whitelist`:
  `EVEPRAISAL_HTTPS_DOMAIN_WHITELIST="example.org www.example.org"`. The same
  holds for the content of a `_FILE`.

The image sets `EVEPRAISAL_DB_PATH` and `EVEPRAISAL_BACKUP_PATH`, so
`EVEPRAISAL_DB_PATH_FILE` and `EVEPRAISAL_BACKUP_PATH_FILE` stop the start
inside it; override the plain variables instead.

Compose secrets keep the cookie keys and the SSO secret out of the config
file and out of `docker inspect`. Compose mounts each one read-only at
`/run/secrets/<name>`, which works with the read-only root filesystem. Put
this into `docker-compose.override.yml` next to `docker-compose.yml`, with
the files under `./secrets/` owned by UID 65532 and readable only by it
(`sudo chown 65532:65532 secrets/* && sudo chmod 0400 secrets/*`):

```yaml
services:
  evepraisal:
    environment:
      EVEPRAISAL_COOKIE_AUTH_KEY_FILE: /run/secrets/cookie_auth_key
      EVEPRAISAL_COOKIE_ENCRYPTION_KEY_FILE: /run/secrets/cookie_encryption_key
      EVEPRAISAL_SSO_CLIENT_ID: your-client-id
      EVEPRAISAL_SSO_CLIENT_SECRET_FILE: /run/secrets/sso_client_secret
    secrets: [cookie_auth_key, cookie_encryption_key, sso_client_secret]
secrets:
  cookie_auth_key:
    file: ./secrets/cookie_auth_key
  cookie_encryption_key:
    file: ./secrets/cookie_encryption_key
  sso_client_secret:
    file: ./secrets/sso_client_secret
```

Generate the cookie keys once and keep them: a new `cookie_auth_key` logs
every user out. `openssl rand -hex 32 > secrets/cookie_auth_key` and
`openssl rand -hex 16 > secrets/cookie_encryption_key` write 64 and 32
characters plus a newline; the newline is removed on read, so the
encryption key is 32 bytes.

### Keys

All values are read once at start; a change needs a restart. The table
follows `configKeys` in `evepraisal/env_config.go`; a test fails when a key
read through viper is missing there or a row is missing here.

| Key | Environment variable | Default | Effect |
| --- | --- | --- | --- |
| `base-url` | `EVEPRAISAL_BASE_URL` | `http://127.0.0.1:8080` | Public URL of the site; used for links and the SSO callback `<base-url>/oauthcallback` |
| `http_addr` | `EVEPRAISAL_HTTP_ADDR` | `:8080` | Listen address of the HTTP server; empty disables it, and then `healthcheck` always fails |
| `http_redirect` | `EVEPRAISAL_HTTP_REDIRECT` | `false` | `true` answers every HTTP request with a 307 to the same host and path on `https://`; only for `https_addr`, never behind a TLS-terminating proxy |
| `https_addr` | `EVEPRAISAL_HTTPS_ADDR` | empty | Listen address of the HTTPS server with Let's Encrypt certificates; empty disables HTTPS; see [HTTPS without a proxy](#https-without-a-proxy) |
| `https_domain-whitelist` | `EVEPRAISAL_HTTPS_DOMAIN_WHITELIST` | `["evepraisal.com"]` | Domains for which certificates are requested |
| `letsencrypt_email` | `EVEPRAISAL_LETSENCRYPT_EMAIL` | empty | Contact e-mail for Let's Encrypt |
| `db_path` | `EVEPRAISAL_DB_PATH` | `db/` (`/data/db/` in the image) | Directory of the databases, created on start |
| `backup_path` | `EVEPRAISAL_BACKUP_PATH` | `db/backups/` (`/data/db/backups/` in the image) | Directory of `appraisals.gz` written by the management API, created on start |
| `esi_baseurl` | `EVEPRAISAL_ESI_BASEURL` | `https://esi.evetech.net` | ESI base URL; one trailing `/latest`, `/v1` or `/legacy` is stripped with a `WARN` log line |
| `market-fetch-interval` | `EVEPRAISAL_MARKET_FETCH_INTERVAL` | `30` | Minutes between the starts of two market price refreshes, a whole number above zero. Anything else in the file (`0`, `-5`, `10m`, `1.5`, `""`) stops the start with `Invalid config: market-fetch-interval must be a whole number of minutes above zero, got "<value>"` |
| `management_addr` | `EVEPRAISAL_MANAGEMENT_ADDR` | `127.0.0.1:8090` | Listen address of the management API, see [Management API](#management-api) |
| `newrelic_app-name` | `EVEPRAISAL_NEWRELIC_APP_NAME` | `Evepraisal` | New Relic application name |
| `newrelic_license-key` | `EVEPRAISAL_NEWRELIC_LICENSE_KEY` | empty | New Relic license key; empty disables New Relic |
| `site-name` | `EVEPRAISAL_SITE_NAME` | `Evepraisal` | Site name in the navbar and page titles |
| `source-url` | `EVEPRAISAL_SOURCE_URL` | `https://github.com/evepraisal/go-evepraisal` | Source repository; the footer links to it and to its `/issues` |
| `about-contacts-html` | `EVEPRAISAL_ABOUT_CONTACTS_HTML` | the four upstream contact items | HTML of the contact list on `/about`, inserted unescaped |
| `favicon-path` | `EVEPRAISAL_FAVICON_PATH` | empty | File served at `/favicon.ico`; empty serves the built-in icon; an unreadable file stops the start |
| `extra-html-header` | `EVEPRAISAL_EXTRA_HTML_HEADER` | empty | HTML added to the page head |
| `extra-js` | `EVEPRAISAL_EXTRA_JS` | empty | JavaScript added to every page |
| `ad-block` | `EVEPRAISAL_AD_BLOCK` | empty | HTML of the ad block |
| `extra-static-file-path` | `EVEPRAISAL_EXTRA_STATIC_FILE_PATH` | empty | Directory of extra files, each served at its path relative to the directory (`<dir>/a/b.txt` at `/a/b.txt`). A symlink to a directory is followed as the path itself. Inside the directory a symlink is served when it resolves to a regular file inside the directory (a Kubernetes ConfigMap mount, links into a release directory); any other symlink is skipped with `WARN: Extra static symlink skipped, not a regular file inside the directory path=<link>`. FIFOs and devices are skipped silently, and so are directories and symlinks whose name starts with `..` (the ConfigMap internals `..data` and `..<timestamp>`, whose files the top-level links serve). A missing path, a file instead of a directory, or any error walking the tree (such as an unreadable subdirectory) stops the start with `Invalid config: extra-static-file-path "<path>": <reason>`. Files are read into memory on start; files added later are not served until a restart |
| `cookie-auth-key` | `EVEPRAISAL_COOKIE_AUTH_KEY` | empty | Session cookie signing key; empty generates a random key on every start, which logs every user out on restart |
| `cookie-encryption-key` | `EVEPRAISAL_COOKIE_ENCRYPTION_KEY` | empty | Session cookie encryption key, used only with `cookie-auth-key`: empty means signed but unencrypted cookies, otherwise 16, 24 or 32 bytes (AES); another length stops the start with `Invalid config: cookie-encryption-key must be empty or 16, 24 or 32 bytes, got <n>` |
| `sso-client-id` | `EVEPRAISAL_SSO_CLIENT_ID` | empty | EVE SSO client ID; empty disables login: `/login` answers `SSO is not configured` |
| `sso-client-secret` | `EVEPRAISAL_SSO_CLIENT_SECRET` | empty | EVE SSO client secret |
| `sso-authorize-url` | `EVEPRAISAL_SSO_AUTHORIZE_URL` | `https://login.eveonline.com/oauth/authorize` | SSO authorize endpoint |
| `sso-token-url` | `EVEPRAISAL_SSO_TOKEN_URL` | `https://login.eveonline.com/oauth/token` | SSO token endpoint |
| `sso-verify-url` | `EVEPRAISAL_SSO_VERIFY_URL` | `https://login.eveonline.com/oauth/verify` | SSO verify endpoint |

## Data directory

Everything lives under `db_path` (`./data/db/` on the host with the compose
file). The bolt databases and `backups/appraisals.gz` are created with mode
0600, the SDE archive and some search index files with the process umask
(0644 under Docker's default 022); `db_path` is created with mode 0700 when
the service creates it; the image's own `/data/db`, used without a bind
mount, has 0755. Everything is owned by UID 65532: on the host, reading it
needs `sudo`. Each bolt database is written by one process at a time;
`evepraisal restore` opens the type DB read-only next to the service.

| Name | Content | Safe to delete |
| --- | --- | --- |
| `appraisals` | all appraisals and user history | **no: the only copy of user data** |
| `prices` | aggregated market prices | with the service stopped; refilled by the first market refresh after start, until then appraisals show no prices |
| `httpcache` | cached ESI responses | with the service stopped; rebuilt on demand |
| `types-<build>` | item types of SDE build `<build>` | with the service stopped; then the next start rebuilds it, see [First start](#first-start-on-an-empty-data-directory) |
| `types-<build>.index` | search index of the same build | only together with `types-<build>` |
| `types-<build>.zip` | the downloaded SDE archive, reused if the build has to be rebuilt | yes |
| `types-<build>.partial*`, `types-<build>.zip.download` | an unfinished build or download | yes; removed on start |
| `certs/` | Let's Encrypt certificates, only with `https_addr` | with the service stopped; certificates are requested again, subject to Let's Encrypt rate limits |
| `backups/appraisals.gz` | output of `/backup-to-file/appraisals` | yes |

After a newer type DB is in use the service removes all other `types-*`
entries. A `types-<md5>` name is a type DB built by upstream Evepraisal; it is
served until the first numeric build is ready, then removed.

## Operations

### First start on an empty data directory

With no ready type DB under `db_path` the service downloads the SDE archive
and builds the type DB before the HTTP server listens. The archive is about
100 MB (a code comment, not measured here); the build takes minutes,
depending on bandwidth and disk (not measured). The image healthcheck has a
30-minute start period: until then a failed probe does not mark the container
unhealthy. It allows 2 x 600 seconds of download (below) plus 10 minutes for
the build, which is not measured; the manifest request before the download
uses the same client and is not counted. A build still running after 30
minutes marks the container unhealthy while it works, after three failed
probes 30 seconds apart (Docker defaults), about 31.5 minutes after start.
A failed build lets the HTTP server start, so the probe passes from then
on.

The download has 600 seconds per attempt and two attempts, 2 seconds apart.
Below about 170 KB/s (100 MB in 600 s) it cannot finish, and every retry
starts the download from zero.

If the first build fails (CCP unreachable, download too slow, broken
archive), the service still starts, but recognises no items until a build
succeeds. It retries after 5 minutes, doubling the pause on each failure up
to 6 hours. Look for `ERROR: failed to fetch static data from CCP:` in the
log.

With a ready type DB on disk the service starts at once on it and checks for
a newer SDE build in the background; it checks again every 6 hours. A failed
check or build keeps the current type DB and logs
`ERROR: failed to fetch static data from CCP, keeping the current type DB:`
for the check right after start, and
`WARNING: Fetcher failed to run, keeping the current type DB:` for the later
ones.

A type DB that exists but fails to open (a damaged file, for example) is
skipped on start: the service runs on the newest older `types-*` that opens,
if there is one, and otherwise builds as on an empty data directory. A broken
DB of the latest SDE build is rebuilt from the archive with
`WARNING: rebuilding type DB that fails to open` in the log; the broken files
stay until the rebuild succeeds. A DB another process holds open for writing
is left alone and retried on the next check.

### Market prices

Every `market-fetch-interval` minutes the service reads all market orders of
six regions from ESI and aggregates them into these markets:

| Market | Source |
| --- | --- |
| `jita`, `perimeter` | The Forge (10000002), systems Jita and Perimeter |
| `amarr` | Domain (10000043), Amarr system and three trade hub stations |
| `dodixie` | Sinq Laison (10000032), Dodixie system and two stations |
| `hek` | Metropolis (10000042), five Hek stations |
| `rens` | Heimatar (10000030), Rens and one more system |
| `universe` | all orders of the five regions above and of region 10000027, which has no market of its own |

Per region and cycle one log line reports the result, for example (values
illustrative):

```text
region=10000002 pages=402 orders=401234 duration=1m2.345s error=""
```

A non-empty `error` means the region failed: its markets keep the previous
prices, `universe` is not updated in that cycle, and the cycle logs:

```text
ERROR: 1 of 6 regions failed, keeping the previous prices of their markets and universe
```

A cycle that lasts longer than the interval is followed by the next one at
once.

Each ESI request has a 30-second timeout. A network error or a 5xx answer is
retried up to 10 attempts with pauses of about 2, 4, 8 ... 512 seconds
(plus or minus a third). One failing page fails its region after
2 + 4 + ... + 512 = 1022 seconds of pauses (681 to 1363 with the jitter)
plus up to 10 x 30 seconds of timed-out attempts: about 11 to 28 minutes,
typically 17 to 22. A 404 counts as an empty page. Any other 4xx except
420 and 429 fails the region at once.

ESI rate limits are honoured inside a cycle, which makes the cycle longer, not
failed. Every pause is capped at 5 minutes.

| ESI signal | Pause |
| --- | --- |
| 420 or 429 | `Retry-After`, else `X-Esi-Error-Limit-Reset` (420 only), else 1 minute; after the third such answer for one URL its region fails (`/markets/prices`: the CCP price fallback is skipped for the cycle) |
| `X-Esi-Error-Limit-Remain` below 10 | until `X-Esi-Error-Limit-Reset` |
| `X-Ratelimit-Remaining` below 100 | 1 minute |

Requests carry `X-Compatibility-Date: 2020-01-01`.

### Healthcheck

`evepraisal healthcheck` requests
`http://127.0.0.1:<port of http_addr>/robots.txt` with a 5-second timeout.
Exit code 0: status 200 or a redirect (with `http_redirect = true` the probe
gets a 307 and counts it as healthy). Exit code 1: anything else, with
`unhealthy: <reason>` on stderr. The image runs it as its `HEALTHCHECK`.

The probe always goes to `127.0.0.1`: with `http_addr` bound to another
address (`192.0.2.10:8080`) or empty it always fails.

### Verify a running instance

`scripts/verify-instance.sh` checks a deployed site from outside; it needs
`sh` and `curl`:

```sh
sh scripts/verify-instance.sh https://example.org 'My Site' 30
```

| Check | Passes when |
| --- | --- |
| `robots` | `/robots.txt` answers 200 |
| `favicon` | `/favicon.ico` answers 200 |
| `branding` | `/about` contains the site name given as the second argument |
| `no-status-link` | `/about` does not link to `status.evepraisal.com` |
| `prices-fresh` | prices of Tritanium (`/item/34`) are at most interval + 10 minutes old; skipped without the third argument |

Exit code 0: all checks passed. 1: the failed check names follow
`failed:`. 2: wrong arguments.

### Stop

On SIGTERM (`docker compose stop`) the service stops the management API,
the HTTP and the HTTPS server, one after the other, giving in-flight requests
up to 10 seconds per server. It then waits for a running SDE download to
end (up to 600 seconds per attempt, the download cannot be interrupted) and
closes the type DB, closes the appraisal DB, cancels running ESI requests, and closes the HTTP
cache and the price DB last. Compose sends SIGKILL after 30 seconds
(`stop_grace_period`). A kill loses no committed data: bolt writes each
transaction atomically (bolt documentation); an unfinished type DB build is
removed on the next start.

### Upgrade

For an installation made from this repository:

```sh
git pull
docker compose up -d --build
```

For an installation running the [published image](#published-image), set the
new tag in `image:` of `docker-compose.yml`, then:

```sh
docker compose pull
docker compose up -d
```

To roll back, set the previous tag and run the same commands. `edge`
follows `master` without a tag change: `docker compose pull` fetches the
newest commit.

The data directory is reused as is. To roll back, check out the previous
commit and run the same `docker compose up -d --build`. Every commit since
the fork reads the same data directory; rolling back to upstream Evepraisal
is not supported: its compose file keeps data inside the container, and its
type DB loader downloads from an S3 URL this fork stopped using, whose
availability is unknown.

### Migrate from the upstream compose file

The upstream compose file keeps the databases in `/evepraisal/db` inside the
container `evepraisal`, not on the host. Removing that container loses them,
so copy them out first. Run from the clone of this repository, with the old
container still present:

1. Stop the old container and copy its databases out:

   ```sh
   docker stop evepraisal
   docker cp evepraisal:/evepraisal/db ./db-upstream
   ```

   Check: `ls db-upstream` lists `appraisals`. If it does not, stop here;
   the old container is still intact.

2. Move them into the data directory and hand it to the container user:

   ```sh
   mkdir data
   mv db-upstream data/db
   sudo chown -R 65532:65532 data
   ```

3. Carry the old config over the clone's `evepraisal.toml`, which compose
   mounts read-only at `/etc/evepraisal/evepraisal.toml`. Without it the
   service runs on the sample config: `base-url` is `http://127.0.0.1:8080`,
   and with no `cookie-auth-key` every restart logs all users out.

   ```sh
   docker cp evepraisal:/evepraisal/evepraisal.toml ./evepraisal.toml
   chmod 644 evepraisal.toml
   ```

   Delete the `db_path` and `backup_path` lines from it: they name the
   upstream `/evepraisal/db` paths, and the image overrides both through the
   environment anyway. The file must stay readable by UID 65532; an
   unreadable one stops the start with `Invalid config file:`.

4. Remove the old container and start the new one:

   ```sh
   docker rm evepraisal
   docker compose up -d --build
   ```

The new service serves the upstream `types-<md5>` type DB until it has built
its first numeric one, then removes the old one. Until `docker rm`, the old
container can be started again with `docker start evepraisal`; after it, the
copy in `data/db` is the only one.

### Back up appraisals

`data/db/appraisals` is the only file that holds user data. Copy it with the
service stopped: a copy of a bolt file taken while it is written can be
inconsistent.

```sh
docker compose stop evepraisal
sudo cp -p data/db/appraisals "appraisals-$(date +%F)"
docker compose start evepraisal
```

The site is down from `stop` until the container answers again; with a type
DB on disk the start does not wait for a build. To restore, stop the
service, put the file back and start it:

```sh
docker compose stop evepraisal
sudo cp appraisals-2026-11-12 data/db/appraisals
sudo chown 65532:65532 data/db/appraisals
docker compose start evepraisal
```

The management API writes a hot backup with `GET /backup-to-file/appraisals`:
a gzip of the bolt file, written to a temporary file and renamed to
`<backup_path>/appraisals.gz` only when complete, so a failed run keeps the
previous backup. With the default compose file the API is reachable only
from inside the container, and the image has no HTTP client to call it;
publishing it needs a changed `management_addr` and network setup, and the
API has no authentication. Restore such a file with
`gunzip -c appraisals.gz > appraisals` and the restore steps above.

### Import legacy appraisals

`evepraisal restore` imports appraisals exported from the original Python
Evepraisal: a gzip of a CSV file with a header row. It skips private
appraisals. It does not read `backups/appraisals.gz`; that file is a bolt
database, restored as in [Back up appraisals](#back-up-appraisals).

Run it while the service is up and healthy: the command sends each appraisal
to the management API on `127.0.0.1:8090`, reachable only inside the service
container, and uses the service's ready type DB without building one. Place
the files under `./data` first so the container sees them:

```sh
docker compose exec evepraisal evepraisal restore -files /data/legacy-1.csv.gz,/data/legacy-2.csv.gz
```

| Outcome | Exit code | Log |
| --- | --- | --- |
| all files read, every row stored or skipped | 0 | `Done restoring: <file> skipped_existing=<n> failed=0` per file |
| a row refused or not sent | 1 | `Restore finished with failed appraisals: failed=<n>` after all files |
| no ready type DB yet | 1 | `Couldn't open type DB, start the service and wait for its first type DB` |
| missing file, unreadable gzip or CSV | 1 | `File path does not exist:` or `Error while importing legacy file:` |
| missing `-files` | 1 | `The -filenames option is required` |

A row the service rejects with a status other than `409` does not stop the
import: it logs `ERROR: <status>: <body>`, counts in `failed` and goes on; a
row the management API does not answer, as during a service stop, logs
`Could not save appraisal!` and counts the same way. Any failed row makes the
command exit 1 once all files are read. Files listed before a failing one stay
imported, and so do the rows of the failing file read before the error.

Each appraisal keeps its legacy ID. An ID the database already holds is
skipped, never overwritten: `/restore` answers `409 Conflict` and the command
counts it in `skipped_existing`, so running the same import again stores
nothing new and does not raise the appraisal total. Legacy IDs share the ID
space with new appraisals; each import moves the ID counter past the highest
imported ID, so a new appraisal never takes an imported one's ID.

### Management API

Listens on `management_addr` (`127.0.0.1:8090`), without authentication. Do
not bind it to a public address.

| Route | Effect |
| --- | --- |
| `GET /backup/appraisals` | streams the bolt file of the appraisal DB |
| `GET /backup-to-file/appraisals` | writes a complete gzip of it to `<backup_path>/appraisals.gz`, see [Back up appraisals](#back-up-appraisals) |
| `POST /restore` | stores one appraisal given as JSON, `409 Conflict` if its ID is taken; used by `evepraisal restore` |
| `GET /expvar` | Go runtime variables |

## Development

Requirements: Go 1.27 (`go.mod`), GNU make. Dependencies are vendored in
`vendor/`; Go uses them automatically (vendor mode is the default when
`vendor/` exists), so building and testing the service needs no network.

### Build and run

```sh
make build
./target/evepraisal-linux-amd64
```

`make build` defaults to `ENV=dev`, which compiles with `-tags dev`: templates
and static files are read from `web/resources/` on disk at run time, so start
the binary from the repository root. A binary that embeds them:

```sh
make build ENV=prod
```

With `ENV=prod` the same target builds
`target/evepraisal-<GOOS>-<GOARCH>` without the `dev` tag.

| Target | Effect |
| --- | --- |
| `make run` | installs the binaries into `$GOPATH/bin` and starts `evepraisal` |
| `make run-reload` | rebuilds and restarts on source changes; needs `reflex` |
| `make test-reload` | reruns the tests on source changes; needs `reflex` |

### Checks before a commit

```sh
make check
```

CI runs the same target. It needs network access on the first run (tool
modules, the Go vulnerability database, `uvx`) and these programs on `PATH`:
`git`, `uv`, `hadolint`, `shellcheck`. Go tools are pinned in `tools/go.mod`
and `tools/actionlint/go.mod`, `rumdl` by version in the `Makefile`.

| Target | Checks |
| --- | --- |
| `lint` | `golangci-lint run` and `golangci-lint fmt --diff` (`.golangci.yml`) |
| `build` | the binary for `ENV`, then `go build ./...` |
| `test` | `go test -race ./...` with checkptr disabled |
| `tidy` | `go mod tidy -diff` for the main and tool modules; `vendor/` matches `go mod vendor` |
| `markdown` | `rumdl check .` (`.rumdl.toml`) and no typographic characters outside data files |
| `hadolint` | `Dockerfile` |
| `actionlint` | `.github/workflows/` |
| `shellcheck` | every `*.sh` outside `vendor/` |
| `vuln` | `govulncheck ./...`; `make check` only prints its findings, `make vuln` fails on them |

The race detector runs with checkptr disabled, because the vendored
`github.com/boltdb/bolt` fails it
(`fatal error: checkptr: converted pointer straddles multiple allocations`).

### Dependencies

After changing imports or `go.mod`, refresh the vendor directory and commit
it together with `go.mod` and `go.sum`:

```sh
go mod tidy && go mod vendor
```

### Ignore files

`.gitignore` and `.dockerignore` deny everything, then re-include what
belongs. A new file or directory at the repository root stays invisible to
git until `.gitignore` gets a `!/<name>` line (`!/<name>/` for a directory);
add it to `.dockerignore` as well if the image build needs it. Check that no
tracked file is ignored:

```sh
git ls-files | git check-ignore --stdin --no-index
```

The command prints nothing when the allowlist is complete.
