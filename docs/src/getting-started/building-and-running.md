# Building & Running

## Prerequisites

- **Go 1.26** or newer.
- The build is **pure Go** — `CGO_ENABLED=0` is enforced globally by the
  Makefile so that no C-toolchain dependency can sneak into the build graph.
- [mdBook](https://rust-lang.github.io/mdBook/) is only needed to build *this*
  documentation, not the binary.

## Build

```sh
make build
```

This produces the CLI at `bin/dashspec` (built with `-trimpath` and stripped
linker flags). `make` with no target also builds, since `build` is the default
goal.

Other Makefile targets mirror the conventions described in
[Conventions](conventions.md): `make test`, `make vet`, `make lint`
(vet + staticcheck), `make cover`, `make fmt`, and `make bench`.

## `resolve`

Validate a dashboard document and print its resolved tree as JSON:

```sh
dashspec resolve examples/minimal-dashboard.json
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--schemas <dir>` | `schemas` | Directory holding `dashboard.schema.json` and the item/connection catalog. |
| `--json` | off | Emit a failing error as a machine-readable JSON envelope (`{code,message,details}`) on stderr instead of the `CODE: message` text form. |

On success, the resolved-tree JSON is written to stdout and the process exits 0.
On the first validation failure, the error is printed and the process exits
non-zero. The `--json` flag is global, so it goes before the subcommand:

```sh
dashspec --json resolve examples/minimal-dashboard.json
```

### Documents with secrets

A document whose connections reference secrets via `{ "$secret": "NAME" }`
requires those environment variables to be set at resolution time, because the
resolved value is needed to validate the connection config. For example,
`examples/kitchen-sink-dashboard.json` references `METRICS_API_TOKEN`:

```sh
METRICS_API_TOKEN=xyz dashspec resolve examples/kitchen-sink-dashboard.json
```

The token's **value never appears in the output** — only the `$secret`
reference object and a sorted list of consumed secret names are kept. See
[Connections](../format/connections.md#secret-handling).

## `serve`

Serve a document over HTTP, re-resolving it on each request:

```sh
dashspec serve examples/dropdown-dashboard.json
# lattice serving examples/dropdown-dashboard.json on http://localhost:8080
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `--schemas <dir>` | `schemas` | Catalog directory (as for `resolve`). |
| `--port <n>` | `8080` | TCP listen port (1–65535). |
| `--json` | off | JSON error envelope for invocation errors. |

The server re-resolves on every request, so editing the document and reloading
the page reflects the change, and a resolution error renders as an HTML error
page (a rendered coded error) rather than crashing the server. Runtime variable
overrides — dropdown selections and `?var=value` URL query parameters — are
threaded into resolution per request, which is what drives the live re-resolve
loop. See [Variables — Runtime inputs](../format/variables.md#runtime-inputs).

This server is a deliberately minimal **structural sketch**, not a rendering
engine; see [Out of Scope](../reference/out-of-scope.md).
