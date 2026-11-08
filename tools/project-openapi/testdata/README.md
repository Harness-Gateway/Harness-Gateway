# Pinned upstream OpenAPI input

`openai-85a23866.json.gz` contains the original bytes of OpenAI's official
`openapi.json` at commit `85a23866c06a29098083ebb11d5ec70438305def`, compressed
with gzip (timestamp zero). It is a test input, not generated gateway code.

Source: <https://raw.githubusercontent.com/openai/openai-openapi/85a23866c06a29098083ebb11d5ec70438305def/openapi.json>

SHA-256 of the uncompressed input:
`0e4dc3d7f52cc7d7c41940587b5ae23c0e3173bd4b4384f9d673ee90185ebafb`.

The upstream MIT license is retained in [api/LICENSE.openai](../../../api/LICENSE.openai).
`TestPinnedUpstreamProjection` decompresses this input, verifies its checksum
through the real projector, and compares the entire projection with the
published gateway schema. CI runs it explicitly on Linux, macOS, and Windows,
without downloading a mutable upstream file.

To reproduce the projection on a Unix development host:

```bash
gzip -dc tools/project-openapi/testdata/openai-85a23866.json.gz > /tmp/harness-gateway-openapi.json
go run ./tools/project-openapi -input /tmp/harness-gateway-openapi.json
go generate ./api
go test ./tools/project-openapi
```

When deliberately updating upstream, update the source commit/checksum constants,
replace this compressed input with the exact new upstream bytes, retain its
license, update this provenance, and regenerate both the projection and Go types.
