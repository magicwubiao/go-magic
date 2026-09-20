# go-magic Scoop Manifest

This file belongs to the bucket repository `magicwubiao/scoop-bucket` (this repository only
holds the source file; the bucket repository has not been created yet, see the notes below).

## Usage (once the bucket is published)

```powershell
scoop bucket add magic https://github.com/magicwubiao/scoop-bucket
scoop install magic
```

## Notes

- Asset names match CI: `go-magic-windows-amd64.exe` / `go-magic-windows-arm64.exe`
  (CI only releases amd64 and arm64, never 386)
- `hash` uses the sha256 digest from the GitHub Release; `autoupdate` builds the new URL
  from `$version`
- `bin` uses the `[["source file", "alias"]]` form to name the shim `magic`, so there is
  **no need** to rename files in post_install
- This file must stay **strict JSON** (no `#` comments -- Scoop parses it with
  `ConvertFrom-Json`)
