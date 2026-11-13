# Changelog

Changes of this fork against upstream
[evepraisal/go-evepraisal](https://github.com/evepraisal/go-evepraisal) at
commit `1aa3024`, for operators moving from upstream. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- The container image is distroless, runs as UID 65532 with a read-only root
  filesystem and all capabilities dropped. Data moved from `/evepraisal/db`
  inside the container to `/data/db` on the bind mount `./data`, the config
  file to `/etc/evepraisal/evepraisal.toml` (read-only). Upstream kept the
  databases only inside the container: copy them out before removing it, see
  "Migrate from the upstream compose file" in `README.md`.
- The image builds the checked-out tree instead of cloning upstream at build
  time, so `docker compose up --build` runs from a clone of this repository.
- Item types come from CCP's JSONL static data export, found through the
  manifest at
  `https://developers.eveonline.com/static-data/tranquility/latest.jsonl`;
  this replaces the fixed S3 `sde.zip` URL. Type DBs are named
  `types-<build>`; an upstream `types-<md5>` is served until the first
  numeric build is ready, then removed.
- Item volumes and packaged volumes come from the SDE itself;
  `fuzzwork.co.uk` is no longer contacted.
- With a type DB on disk the service starts on it at once and builds a newer
  SDE build in the background, checking every 6 hours; a failed build keeps
  the current type DB. Older `types-*` entries are removed once a newer one is
  in use; unfinished builds and downloads are removed on start.
- A failed first type DB build is retried after 5 minutes, doubling up to
  6 hours, instead of after a flat 6 hours.
- Market data uses the dated ESI API: requests go to the base URL without a
  version path and send `X-Compatibility-Date: 2020-01-01`. A trailing
  `/latest`, `/v1` or `/legacy` in `esi_baseurl` is stripped with a warning,
  so existing configs keep working; the default is `https://esi.evetech.net`.
- ESI rate limits are honoured: a 420 or 429 pauses for `Retry-After` or the
  error-limit reset (1 minute without a header, at most 5 minutes) and is
  retried up to three times; a low error or rate budget pauses before the
  next request. A busy cycle gets longer instead of being blocked by ESI.
- Region pages are counted from `X-Pages` instead of reading until an empty
  page.
- One failing region no longer discards the whole cycle: the other regions'
  markets are updated, the failed region's markets and `universe` keep their
  previous prices. Each region logs
  `region= pages= orders= duration= error=` per cycle.
- `restore` runs next to the running service (`docker compose exec`): it
  opens the service's ready type DB instead of downloading and building its
  own, and stops with an error if there is none yet.
- The item page links market data to EVE Tycoon instead of EVEMarketer, whose
  link no longer led to market data.
- The footer no longer links to the upstream status page and blog.
- Go 1.27 (upstream 1.19); the stale `Gopkg.*` files are gone. `make check`
  runs every check CI runs; the `setup`, `dist` and `deploy-prod` targets and
  `scripts/deploy.sh` are gone (`make build ENV=prod` replaces `make dist`).

### Added

- `market-fetch-interval`: minutes between market refreshes, default `30`
  (the former fixed value). A value that is not a whole number above zero
  stops the start.
- `site-name`, `source-url`, `about-contacts-html`, `favicon-path`: branding
  of a self-hosted instance. Defaults reproduce the upstream site.
- `evepraisal healthcheck` subcommand, used by the image `HEALTHCHECK` with a
  15-minute start period that covers the first type DB build on an empty
  volume. It does not follow redirects and counts a 3xx as healthy, so it
  works with `http_redirect = true`.
- `scripts/verify-instance.sh`: checks a deployed site from outside
  (robots, favicon, branding, status link, price freshness).
- The service stops cleanly on SIGTERM (`docker stop`), not only on SIGINT;
  compose waits 30 seconds (`stop_grace_period`, 10 per server) before
  SIGKILL.
- A published image `ghcr.io/6run0/go-evepraisal` for `linux/amd64`, built
  after `make check` passes: `X.Y.Z` and `X.Y` from tags `vX.Y.Z` on
  `master`, `latest` from the highest such tag, `edge` and `sha-<commit>` from
  every push to `master`. Each image carries an
  SBOM and a provenance attestation, verifiable with `gh attestation verify`;
  see "Published image" in `README.md`.
- Portable environment variable names: `EVEPRAISAL_` plus the key
  upper-cased with `-` replaced by `_` (`EVEPRAISAL_BASE_URL`), which a POSIX
  shell can `export`. The names with `-` (`EVEPRAISAL_BASE-URL`) still work;
  both set to different values stop the start with `Invalid environment`.
- `<variable>_FILE` reads a key from a file, for Docker or compose secrets
  (`EVEPRAISAL_COOKIE_AUTH_KEY_FILE=/run/secrets/cookie_auth_key`); one
  trailing line ending is removed. A variable and its `_FILE` both set, an
  unreadable file, or an empty file for `db_path`, `backup_path`,
  `market-fetch-interval` or `http_redirect` stop the start. See
  "Environment variables" in `README.md`.
- An `EVEPRAISAL_*` variable that matches no key logs
  `WARN: Unknown environment variable ignored name=<variable>` at start.
- The startup settings log lists keys set only through the environment too,
  in key order, TOML table keys as `<table>.<key>`. Values of
  `cookie-auth-key`, `cookie-encryption-key`, `sso-client-secret`,
  `newrelic_license-key` and of any key whose name contains `key`, `secret`,
  `password` or `token` appear as `<set>` or `<unset>` instead of `MASKED`;
  the four named keys are listed even when unset.

### Fixed

- A missing `db_path` or `backup_path` directory, as on a fresh bind mount,
  no longer crash-loops the service: both are created on start and before
  `restore`.
- A syntax error or unreadable `evepraisal.toml` stops the start with
  `Invalid config file: <path>: <reason>`; it was ignored, and the service ran
  on defaults. A missing file logs `No config file found, using defaults`,
  which never appeared before.
- `cookie-auth-key` without `cookie-encryption-key` gives signed, unencrypted
  session cookies; every session save failed before and login did not stick.
  An encryption key of a length other than 16, 24 or 32 bytes stops the start.
- `extra-static-file-path` pointing to a missing path or a file stops the
  start with an error naming the key; it panicked. A trailing `/` on the path
  no longer drops the leading `/` of every served route. A symlinked
  directory as the path is served; no file under it was. Any error walking
  the tree, such as an unreadable subdirectory, stops the start.
- Files from `extra-static-file-path` are sent with a `Content-Type` from
  the file extension (sniffed when the extension is unknown), `/robots.txt`
  as `text/plain`, both with `X-Content-Type-Options: nosniff`; the type was
  set on the request, so a stylesheet arrived as `text/plain` and browsers
  did not apply it. Inside the directory a symlink is served only when it
  resolves to a regular file inside the directory, other symlinks are
  skipped with a `WARN` line: a symlink served any file the process could
  read. FIFOs and devices are skipped, a FIFO blocked the start.
  Directories named `..*` are skipped, so a ConfigMap mount serves each
  file once, at its own name.
- A type DB whose search index fails to open closes its bolt file; each
  retry of a failed first build left a descriptor, a memory mapping and the
  file lock behind.
- With `newrelic_license-key` set, the market HTTP client is wrapped for New
  Relic before the price fetcher starts; the wrapper replaced the transport
  while the fetcher was already using it, a data race.
- `/backup-to-file/appraisals` writes a complete gzip file: the stream was
  never closed, so `appraisals.gz` was truncated. It is written to a temporary
  file and renamed only when complete, so a failed run keeps the previous
  backup.
- In-flight HTTP requests get 10 seconds on stop: the shutdown deadline was
  created at start and had long expired by the time of a stop.
- `restore` and `POST /restore` never overwrite an appraisal with the same ID:
  it is skipped with `409 Conflict` and counted in `skipped_existing`. The ID
  counter moves past every imported ID; a new appraisal could take an
  imported ID and replace it. A repeated import no longer raises the
  appraisal total.
- `restore` exits 1 when any row was refused by `/restore` (a status other
  than `409`) or could not be sent; each file logs `failed=<n>` next to
  `skipped_existing`. Such rows were only logged and the command exited 0.
- On stop the management API shuts down before the databases close, giving
  an in-flight `/restore` or `/backup-to-file` the same 10 seconds as the
  other servers; it kept accepting requests against the closed appraisal DB.
- A new appraisal never replaces a stored one. On start the ID counter is
  raised to the highest stored ID, which repairs volumes restored by an
  earlier build (logged as `Appraisal sequence raised to stored ID`); a
  counter that still lands on a taken ID fails the save with
  `appraisal ID sequence behind stored keys`. An imported ID above `2^62`
  is rejected (`imported appraisal ID out of range`), so the counter cannot
  wrap to 0.
