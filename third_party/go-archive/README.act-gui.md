# go-archive compatibility copy

Source: https://github.com/moby/go-archive/tree/v0.3.3

Upstream commit: `ae9e219f7104d91e262055a29bae1f9753106981`

Module checksum: `h1:OxxR9paxsluYi+zDUEXTTaIxtkK3viymW+Ka7vRhhME=`

This directory contains the complete Go module distribution of v0.3.3, including its Apache-2.0 license, tests, and fixtures. The root module replaces `github.com/moby/go-archive` with this directory to retain the security fixes for [GO-2026-6253](https://pkg.go.dev/vuln/GO-2026-6253) while supporting act v0.2.89.

The only production change is `compat.go`: `archive.Uncompressed` aliases `compression.None`. act uses this constant to create Docker build contexts. Archive extraction and path-containment code remain unchanged from upstream.

Run checks from the project root so the application's dependency versions are used:

```sh
go test ./...
go test github.com/moby/go-archive -run 'Test(TarWithOptions|Untar)'
```

Local replacements do not retain upstream vulnerability-version matching in scanners. Review upstream advisories and releases when updating dependencies; a clean application scan alone does not establish the security of this copy. Keep all upstream files unchanged when refreshing it, then retain only the compatibility alias and this provenance note. Remove the replacement and this directory once act supports the upstream compression API.
