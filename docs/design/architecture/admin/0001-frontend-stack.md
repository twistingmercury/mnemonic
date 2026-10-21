# ADR 0001: Frontend Stack and Build Strategy

> Back to [Admin UI Architecture](README.md) | [Project README](../../../README.md)

## Status

Accepted

## Context

The phase-1 Mnemonic Admin UI is a pattern-centric internal web application. It
needs a single primary workspace route, semantic search, pattern detail loading,
and a single-file Markdown import flow that parses files locally and submits
JSON to the Mnemonic API.

The repository is greenfield. The stack should optimize for fast delivery,
simple local development, and low architectural overhead.

## Decision

Use the following frontend stack:

- React 19
- TypeScript
- Vite
- React Router
- TanStack Query
- Tailwind CSS
- Vitest
- React Testing Library
- Playwright
- ESLint (with `typescript-eslint`, `eslint-plugin-react-hooks`, `eslint-plugin-react-refresh`, `eslint-plugin-security`)
- Prettier
- `npm audit` — dependency vulnerability scanning
- Semgrep — static application security testing
- Docker multi-stage builds
- Docker Compose for end-to-end orchestration
- `build/build.sh` as the canonical build orchestrator
- GitHub Actions for the CI workflow

## Rationale

### React 19 + TypeScript

The UI is stateful, form-driven, and component-oriented. React is the fastest
path to the split-pane workspace, import overlay, and detail rendering. TypeScript
is worth the upfront cost because the API has distinct response shapes and the
import flow needs strict payload translation.

### Vite

This app does not need SSR, server components, or backend rendering. It talks
directly to a local API and parses Markdown files in the browser. Vite keeps the
project small and fast to start.

### React Router

Phase 1 only needs one primary route, but routing should exist from the start so
the app can grow cleanly if pattern deep links or secondary screens are added.

### TanStack Query

The product is API-driven. Querying, caching, refetching, loading states, and
error states are first-class concerns. TanStack Query reduces custom request
state code and fits browse, search, detail, and post-import refresh flows well.

### Tailwind CSS

The project needs to move quickly from wireframe to working UI. Tailwind is the
fastest path to a disciplined layout system, responsive behavior, and reusable
visual tokens without building a custom CSS architecture first.

### ESLint + Prettier

ESLint with `typescript-eslint` enforces type-aware lint rules across the codebase. `eslint-plugin-react-hooks` catches incorrect hook usage. `eslint-plugin-react-refresh` is required for Vite's HMR to work correctly. `eslint-plugin-security` catches common security antipatterns such as unsafe regex, `eval` usage, and injection risks. Prettier handles formatting consistently without style debates.

All run as local devDependencies via `npx` or `npm run` scripts and integrate into the Docker-first build and CI workflow.

### npm audit + Semgrep

`npm audit` is built into npm and checks all dependencies against known CVE databases — no install required. Semgrep provides language-agnostic static application security testing with strong JS/TS rules, analogous to `gosec` in Go projects. Both run in CI as quality gates.

### Vitest + React Testing Library + Playwright

Vitest fits a Vite app with low setup overhead. React Testing Library covers UI
behavior and interaction logic. Playwright covers the critical end-to-end flows:
search, selection, related-pattern pivoting, and import.

### Docker-first build orchestration

The canonical build should run in containers, not depend on host-installed
tooling. A Docker-first approach keeps the build environment predictable and
makes the local build path match CI closely.

The build strategy should include:

- a Dockerfile that performs the real frontend build
- a `build/build.sh` script that orchestrates the build and test path
- Docker Compose when the end-to-end test flow needs multiple containers

### GitHub Actions workflow

The repository should have a CI workflow that calls the same orchestrator used
locally. That keeps the build definition thin and prevents CI-only behavior from
drifting away from local developer workflows.

## Rejected Alternatives

### Next.js

Rejected for phase 1 because the app does not benefit from SSR or server-side
data loading. It would add structure and runtime concepts the project does not
need yet.

### No query library

Rejected because browse, search, detail, and refresh behavior would otherwise
push request lifecycle logic into local components.

### CSS Modules or handwritten global CSS as the primary styling system

Rejected for phase 1 because they would slow down initial screen construction.
They remain acceptable for targeted cases if Tailwind alone becomes awkward.

### Host-only build and test workflow

Rejected as the primary build path because it makes local success depend more on
developer machine state and increases the chance that CI behaves differently.

## Baseline Commands

These commands should exist once scaffolding is complete:

```bash
npm install
npm run dev
npm run test
npm run e2e
./build/build.sh
```

## Consequences

- The UI remains a client-side application with a direct dependency on the
  Mnemonic API.
- Import parsing and validation will run in the browser.
- Server state and request handling will be centralized through TanStack Query.
- The canonical build and CI path will be container-based.
- The repository will need Docker build and workflow files in addition to the
  frontend application code.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
