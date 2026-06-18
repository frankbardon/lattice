# Lattice

Collaborative dashboard builder. Open a link, see a live masonry board of "bricks"
(markdown, data charts), edit together in real time. Each brick has its own AI builder
agent; a board-level agent arranges the whole layout.

Single Go binary embedding four in-house systems:

- **[Nexus](https://github.com/frankbardon/nexus)** — agent engines (one per brick + a layout agent).
- **[Parsec](https://github.com/frankbardon/parsec)** — realtime pub/sub transport + presence.
- **[Pulse](https://github.com/frankbardon/pulse)** — data engine, reached as a stdio MCP child process.
- **[Prism](https://github.com/frankbardon/prism)** — server-side spec → SVG chart rendering.

## Architecture

Server-authoritative: clients send intents over Parsec → the server applies an RFC6902 patch
to an in-memory Scene document → snapshots to a SQL store → broadcasts the patch and the
server-rendered fragment to all viewers. The frontend (AlpineJS + Tailwind + DaisyUI) is thin —
it slots server-rendered HTML/SVG and never renders brick content itself. Renderers are pluggable
by `brick.kind`; variables are resolved server-side at render time.

## Status

v0.1.0 in development. Built with the [Flow](https://github.com/frankbardon) planning workflow.
