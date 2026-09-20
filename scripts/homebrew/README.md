# go-magic Homebrew Formula

`Formula/go-magic.rb` belongs to the tap repository `magicwubiao/homebrew-tap`; this repository only holds the source file of the formula.

## Current state (important)

- The tap repository `magicwubiao/homebrew-tap` **has not been created yet**, so both
  `scripts/install.sh --method homebrew` and `scripts/install-homebrew.sh --tap` cannot
  succeed for now and will print a clear message.
- Until it exists, use `scripts/install.sh` (the default binary method) or
  `scripts/install-homebrew.sh --prefix <brew-prefix>` (which installs the binary straight
  into `<prefix>/bin`).

## Usage once the tap is published

```bash
brew tap magicwubiao/tap
brew install magicwubiao/tap/go-magic
```

Or:

```bash
scripts/install.sh --method homebrew
```

## Maintenance rules

1. **Asset names must match CI**: `.github/workflows/release.yml` uploads bare binaries
   `go-magic-<os>-<arch>` (`.exe` on Windows), **not** `.tar.gz` / `.zip`. The formula
   previously pointing at a non-existent `magic-*.tar.gz` was a bug.
2. **version and sha256 must come from the same Release**:
   take `assets[].digest` from the GitHub API or the checksums on the Release page, e.g.:

   ```bash
   curl -fsSL https://api.github.com/repos/magicwubiao/go-magic/releases/latest \
     | grep -E '"name"|"digest"'
   ```

3. When releasing, update `version` and the four `sha256` values (darwin/linux x
   amd64/arm64), or make this file an automated input of the tap repository
   (brew bump / manual PR).
4. `test do` relies on `magic --version` (that flag is explicitly supported by
   `cmd/magic/main.go`); do not change it to any custom output other than `magic version`.
