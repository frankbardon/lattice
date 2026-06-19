# Examples

The `examples/` directory holds hand-written, conforming dashboard documents.
Each is a valid fixture you can resolve directly, and together they cover every
feature of the spec. Paths below are relative to the repository root.

| File | Demonstrates |
| --- | --- |
| [`examples/minimal-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/minimal-dashboard.json) | The smallest real document: a root container with two static tables. |
| [`examples/grids-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/grids-dashboard.json) | Nested grids / subgrids with explicit `placement` and fractional track sizing. |
| [`examples/variables-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/variables-dashboard.json) | All three variable kinds — static, runtime-settable, computed `expr` — feeding `$var` and `${}` references. |
| [`examples/binding-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/binding-dashboard.json) | An item bound to an `http` connection by id with a variable-filled query, plus a `$secret` reference. |
| [`examples/dropdown-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/dropdown-dashboard.json) | The live runtime-input loop: a `select` widget sets an enum variable consumed by a table. |
| [`examples/connections-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/connections-dashboard.json) | Both connection kinds declared at document scope — `static` (inline) and `http` (query) — with `secretRefs`. |
| [`examples/contract-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/contract-dashboard.json) | A table bound to a `static` connection whose inline rows conform to the table's `expectedResult` contract. |
| [`examples/kitchen-sink-dashboard.json`](https://github.com/frankbardon/lattice/blob/main/examples/kitchen-sink-dashboard.json) | Every feature in one document (see below). |

## Resolving an example

Most examples resolve with no setup:

```sh
lattice resolve examples/minimal-dashboard.json
```

## The kitchen-sink example needs a secret

`examples/kitchen-sink-dashboard.json` exercises the whole spec in one document:
nested grids with subgrids and explicit placement; static, runtime, and computed
variables; `$var` typed bindings and `${}` string templates; a `select` runtime
input bound to an enum variable; both connection types; a `$secret` reference that is redacted from the
resolved tree; and a result-shape contract on a bound table.

Because it includes a `$secret`, it requires `METRICS_API_TOKEN` to be set in
the environment to resolve by hand:

```sh
METRICS_API_TOKEN=xyz lattice resolve examples/kitchen-sink-dashboard.json
```

The token value never appears in the output — see
[Connections — Secret handling](../format/connections.md#secret-handling).

## Serving an example

The select and kitchen-sink examples are most interesting under `serve`, where
the runtime-input loop is live:

```sh
lattice serve examples/dropdown-dashboard.json
# then open http://localhost:8080/?region=eu to set the initial value
```
