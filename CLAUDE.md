# CLAUDE.md

Traps of this repository that the tree does not show. Usage and operations
are in `README.md`, user-visible changes in `CHANGELOG.md`.

## Gate before every commit

```sh
make check
```

CI runs the same target; the targets are listed in `README.md`. Only
`vuln` is non-blocking inside `check` (reachable findings in old `x/net`,
`x/text`, `grpc`): its failure is ignored whatever the cause, a tool build or
network error included, so a green `make check` does not mean govulncheck
ran. `make vuln` alone is the strict check.

Tools are pinned in `tools/go.mod` and, because its yaml dependency conflicts
with golangci-lint's, actionlint in `tools/actionlint/go.mod`. Update them
with `go get -tool <pkg>@<version>` inside that directory. The Makefile runs
them through `go -C <dir> tool -n`: `go tool -modfile=tools/go.mod` from the
root loads the root `vendor/` and fails with "inconsistent vendoring".

Lint exclusions in `.golangci.yml` (unchecked `Close`, ST1005,
unused-parameter) exist for upstream code but apply repository-wide; the
reason for each is next to it.

`-race` needs `-gcflags=all=-d=checkptr=0`: the vendored
`github.com/boltdb/bolt` fails checkptr (`fatal error: checkptr: converted
pointer straddles multiple allocations`) and aborts the test binary of every
package that opens a bolt DB (`./staticdump/` among them).

## Vendor mode

Dependencies live in `vendor/`. Go selects vendor mode by itself because
`vendor/` exists; the Dockerfile and `make build` also pass `-mod=vendor`.
After touching imports or `go.mod` run `go mod tidy && go mod vendor` and
commit `vendor/` with `go.mod` and `go.sum`, otherwise `make tidy` and the
image build fail. The module path stays
`github.com/evepraisal/go-evepraisal` although this is a fork.

## Allowlist ignore files

`.gitignore` (`/*`, `.*`) and `.dockerignore` (`*`, `.*`) deny everything
and re-include by name. A new root file or directory is invisible to git until `.gitignore`
gets `!/<name>` (`!/<name>/` for a directory), and absent from the image until
`.dockerignore` gets it too. After editing either file,
`git ls-files | git check-ignore --stdin --no-index` must print nothing.

## Config keys

Environment variables exist only for keys in `configKeys`
(`evepraisal/env_config.go`); viper's `AutomaticEnv` is not used. A new key
needs a row there and in the README key table, otherwise
`TestConfigKeysCoverViperReads` or `TestReadmeListsEnvNames` fails. The scan
sees only string literals passed to `viper.Get*`/`SetDefault`: a key built at
run time escapes it and silently has no environment variable.

## Verbatim test data

`staticdump/testdata/*.jsonl` are lines copied verbatim from SDE build
3687575; tests assert on their exact values (Drake 24698, packaged volume
15000). Never hand-edit or reformat them; replace them only with lines from a
real SDE build and update the asserted values.

## Internal documents

Plans, specs and agent reports live in `docs/superpowers/`, `.superpowers/`
and `.omc/`. All three are kept out of git by the allowlist and written in
Russian; do not link them from `README.md` or `CHANGELOG.md`, readers of
those have no copy.
