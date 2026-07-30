# Check if commit subject is compliant with HAProxy guidelines

[![Contributors](https://img.shields.io/github/contributors/haproxytech/check-commit?color=purple)](https://github.com/haproxy/haproxy/blob/master/CONTRIBUTING)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

This action checks that the commit subject is compliant with the [patch classifying rules](https://github.com/haproxy/haproxy/blob/master/CONTRIBUTING#L632) of HAProxy contribution guidelines. Also it does minimal check for a meaningful message in the commit subject: no less than 20 characters and at least 3 words.

## Examples

### Good

- Bug fix:
```
BUG/MEDIUM: fix set-var parsing bug in config-parser
```
- New minor feature:
```
MINOR: Add path-rewrite annotation
```
- Minor build update:
```
BUILD/MINOR: Add path-rewrite annotation
```

### Bad

- Incorrect patch type
```
bug: fix set-var parsing bug in config-parser
```
- Short commit message
```
BUG/MEDIUM: fix set-var
```
- Unknown severity
```
BUG/MODERATE: fix set-var parsing bug in config-parser
```


## Inputs

None.

## Usage

```yaml
steps:
  - name: check-commit
    uses: docker://ghcr.io/haproxytech/commit-check:TAG
    env:
      API_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```
Check-commit works only on `pull_request` events by inspecting all commit messages in a Pull Request.

### Commit data source

Commits and diffs are read from the local git clone when possible (GitHub, GitLab and local runs alike), which enables attributing file spelling errors to the commit that introduced them. For this to work in CI the clone needs enough history — use `fetch-depth: 0` with `actions/checkout` (GitHub) or `GIT_DEPTH: "0"` (GitLab).

When the clone is missing or too shallow, check-commit falls back to the [pull requests API](https://docs.github.com/en/rest/reference/pulls#list-commits-on-a-pull-request) / GitLab merge requests API (set `API_TOKEN`). If the API is also unavailable, the checks are skipped with a warning: the run stays green and the junit report contains a `commit checks skipped` entry whose body carries the error detail.

## Example configuration

If a configuration file (`.check-commit.yml`) is not available in the running directory, a built-in failsafe configuration identical to the one below is used.

```yaml
---
HelpText: "Please refer to https://github.com/haproxy/haproxy/blob/master/CONTRIBUTING#L632"
PatchScopes:
  HAProxy Standard Scope:
    - MINOR
    - MEDIUM
    - MAJOR
    - CRITICAL
PatchTypes:
  HAProxy Standard Patch:
    Values:
      - BUG
      - BUILD
      - CLEANUP
      - DOC
      - LICENSE
      - OPTIM
      - RELEASE
      - REORG
      - TEST
      - REVERT
    Scope: HAProxy Standard Scope
  HAProxy Standard Feature Commit:
    Values:
      - MINOR
      - MEDIUM
      - MAJOR
      - CRITICAL
TagOrder:
  - PatchTypes:
    - HAProxy Standard Patch
    - HAProxy Standard Feature Commit
```

### Optional parameters

The program accepts an optional parameter to specify the location (path) of the base of the git repository. This can be useful in certain cases where the checked-out repo is in a non-standard location within the CI environment, compared to the running path from which the check-commit binary is being invoked.

### aspell

to check also spellcheck errors aspell was added. it can be configured with `.aspell.yml`

example
```yaml
mode: subject
min_length: 3
ignore_files:
  - go.mod
  - go.sum
  - '*test.go'
  - 'gen/*'
allowed:
  - aspell
  - config
```

`min_length` is minimal word size that is checked (default: 3)

`mode` can be set as

- `subject`
  - `default` option
  - only subject of commit message will be checked
- `commit`
  - whole commit message will be checked
- `all`
  - both commit message and all code committed
- `disabled`
  - check is disabled

### Adding allowed words

```bash
check-commit append <word> [word...]
```

Adds words to the `allowed:` list in `.aspell.yml` (created if missing) and
sorts the whole list. When `remote_file` points at a GitLab wiki page
(`https://host/group/project/-/wikis/page` or the API form), the wiki page is
updated instead, preserving its formatting. Any other remote word source
makes the command refuse and print where to add the word instead.

GitLab authentication uses `private_token_env` / `token_env` when set; when
the variable is missing or empty, the token is read from a locally installed
[glab](https://gitlab.com/gitlab-org/cli) (`glab config get token --host <host>`),
so logged-in users need no extra setup. glab only stores tokens for hosts you
logged into, so the token is never sent to unknown hosts.
