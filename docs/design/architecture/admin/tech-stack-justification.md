# Frontend Tech Stack Justification

> Back to [Admin UI Architecture](README.md) | [Project README](../../../README.md)

This document explains the proposed frontend stack for the Mnemonic Admin web
UI in plain language. It is written for project planning and decision review,
not just for frontend implementation.

Related decision record:
[0001-frontend-stack.md](0001-frontend-stack.md)

## Objective

Choose a frontend stack that is:

- Fast to implement for a greenfield project
- Easy to run locally
- Well suited to a search-first internal application
- Strict enough to keep API integration and file import behavior reliable
- Common enough that future contributors are likely to recognize it

## Recommended Stack

- React 19
- TypeScript
- Vite
- React Router
- TanStack Query
- Tailwind CSS
- Vitest
- React Testing Library
- Playwright
- Docker multi-stage builds
- Docker Compose for end-to-end orchestration
- `build/build.sh` as the canonical build entrypoint
- GitHub Actions for CI

## Executive Summary

This stack is a pragmatic choice, not an experimental one. It is optimized for
building a browser-based application that talks directly to an API, renders a
stateful workspace, performs local file parsing in the client, and builds in a
consistent containerized environment.

The most important point is this: phase 1 does not need server-side rendering,
SEO, or a complex backend-for-frontend layer. It needs a fast, maintainable
client application with a predictable build path. That makes a React and Vite
stack plus a Docker-first build layer the most direct path.

## What Each Piece Does

### React 19

React is the UI framework. It is responsible for rendering the split-pane
workspace, search form, results list, detail pane, and import overlay.

Why it fits this project:

- The UI is interactive and stateful.
- The application is made of reusable panels and controls.
- React is widely used and easy to hire for later.
- It has a mature ecosystem around routing, data fetching, and testing.

What we are not buying:

- React does not solve routing, API caching, or testing by itself.
- Those concerns still need supporting libraries.

### TypeScript

TypeScript adds type checking to JavaScript. It helps catch integration and
data-shape problems before runtime.

Why it fits this project:

- The Mnemonic API has several distinct response shapes.
- The import flow translates Markdown and YAML into a JSON payload.
- Pattern search results and full pattern details are not the same shape.
- Typed code reduces errors when the UI evolves.

Why this matters here:

Without TypeScript, this project would be more fragile. A misspelled field or a
wrong assumption about the API would surface later and more unpredictably.

### Vite

Vite is the frontend build and local development tool.

Why it fits this project:

- It starts quickly.
- It is simple to configure for a client-side app.
- It works well with React, TypeScript, Vitest, and Tailwind.
- It avoids the extra structure of larger frameworks.

Why not something heavier:

This app does not need server rendering or full-stack conventions to be useful.
Vite keeps the project small and fast.

### React Router

React Router manages screen-level navigation.

Why it fits this project:

- Phase 1 only needs one main workspace, but routing should still exist.
- It gives us room for future deep links, saved searches, or secondary screens.
- It is the standard routing choice in many React applications.

Why include it now:

Even if phase 1 is mostly one route, adding routing later is more awkward than
starting with a minimal route structure now.

### TanStack Query

TanStack Query manages API request state, caching, refetching, loading states,
and error states.

Why it fits this project:

- The app is driven by remote data.
- Search results, browse lists, and detail views all require fetch logic.
- Import success should trigger refresh behavior cleanly.
- It reduces handwritten state-management code.

Why it matters:

Without a query library, the code tends to fill up with ad hoc loading flags,
manual retries, duplicated request logic, and brittle refresh behavior.

### Tailwind CSS

Tailwind CSS is the styling system.

Why it fits this project:

- It is fast for building layouts from wireframes.
- It keeps spacing, typography, and layout decisions consistent.
- It works well for internal tools where speed and structure matter.
- It reduces the amount of custom CSS architecture we need to invent.

Tradeoff:

Tailwind can make markup denser. That is acceptable for phase 1 because it buys
speed and consistency. If needed, we can extract reusable UI components as the
design stabilizes.

### Vitest

Vitest is the unit and component test runner.

Why it fits this project:

- It works naturally with Vite.
- It is fast to run.
- It is a good fit for parser, validation, and API client tests.

### React Testing Library

This library tests UI behavior the way a user experiences it.

Why it fits this project:

- The critical behaviors are interactive: searching, selecting, importing,
  seeing errors, and navigating related patterns.
- It helps us test behavior instead of implementation details.

### Playwright

Playwright is the end-to-end testing tool.

Why it fits this project:

- It can verify the full search and import flows in a real browser.
- It is useful for catching integration issues across routing, state, and API
  interactions.
- It is mature and widely used.

### Docker multi-stage builds

Docker provides the canonical build environment.

Why it fits this project:

- It reduces dependency on host machine setup for repeatable builds.
- It aligns local build behavior with CI.
- It matches the engineering handbook preference for containerized builds and
  deployment-oriented artifacts.
- It gives the project a clean path toward shipping a runnable image later.

Why this matters:

For a greenfield project, the build process is part of the architecture. If the
build only works reliably on one developer machine, the project becomes harder
to maintain immediately.

### Docker Compose

Docker Compose is useful when end-to-end tests need the frontend and supporting
services to run together.

Why it fits this project:

- The UI already depends on the Mnemonic API.
- A composed test environment is a practical way to run browser tests against a
  predictable service topology.
- It keeps local and CI orchestration similar.

### `build/build.sh`

This script is the build orchestrator.

Why it fits this project:

- It creates one canonical entrypoint for the build path.
- CI can call the same script that developers use locally.
- It matches the handbook’s recommendation to keep workflow YAML thin and push
  real logic into versioned scripts.

### GitHub Actions

GitHub Actions is the CI workflow layer.

Why it fits this project:

- The repository already lives on GitHub.
- A workflow file can call `build/build.sh` directly.
- It is a thin way to express CI when the real build logic already lives in the
  repository.

## Why This Stack Is a Good Match for the Approved Scope

The approved phase-1 product is:

- A search-first workspace
- A detail pane with related-pattern pivoting
- A single-file Markdown import flow
- A client-side app talking directly to the Mnemonic API

This stack lines up well with that scope:

- React handles the interactive layout.
- TypeScript protects the API and import translation logic.
- Vite keeps the setup lean.
- TanStack Query handles browse, search, detail, and refresh flows.
- Tailwind lets us build the approved screen map quickly.
- The test stack covers parser logic, UI behavior, and end-to-end flows.
- Docker keeps the build path consistent across local development and CI.
- GitHub Actions gives the repo a lightweight, script-driven CI workflow.

## Alternatives Considered

### Next.js

Why it was considered:

- It is popular.
- It offers routing and a lot of built-in structure.

Why it is not the right first choice here:

- This app does not need server-side rendering.
- It does not need SEO.
- It does not need server components.
- It adds concepts and project structure that do not help phase 1.

Next.js is not a bad choice in general. It is just more framework than this
first version needs.

### Plain React without TanStack Query

Why it was considered:

- Fewer dependencies.

Why it was rejected:

- API-driven screens would require more manual state handling.
- Search, refetch, retries, and cache invalidation would be more error-prone.
- The code would get messy faster.

### CSS Modules or handwritten CSS instead of Tailwind

Why they were considered:

- Simpler mental model for some developers.

Why they were rejected for phase 1:

- They slow down initial layout construction.
- They require more up-front CSS organization.
- They provide less immediate structure for a greenfield internal tool.

### Host-only build workflow

Why it was considered:

- It is simpler at the start.

Why it was rejected as the primary path:

- It increases the chance of machine-specific build issues.
- It creates more room for local and CI behavior to diverge.
- It does not match the preferred engineering workflow you referenced.

## Risks and Tradeoffs

No stack is free. These are the main tradeoffs we are accepting.

### React ecosystem complexity

React is powerful, but it is not a batteries-included framework. The benefit is
flexibility. The cost is choosing the right supporting libraries.

Mitigation:

- Keep the stack small and conventional.
- Avoid unnecessary state-management libraries.

### Tailwind can lead to noisy markup

Utility classes can become verbose.

Mitigation:

- Extract repeated patterns into components.
- Keep layout primitives consistent.

### Client-side API dependence

This app assumes the API is reachable from the browser.

Mitigation:

- Centralize API access through one client module.
- Use environment-based base URL configuration from the start.

### Docker-first builds add up-front setup work

Containerized builds are more disciplined, but they do add files and scripts
early in the project.

Mitigation:

- Keep the workflow thin.
- Put real logic in `build/build.sh`.
- Use the same build entrypoint locally and in CI.

### Browser-side file parsing

Import validation happens in the client, which means parsing logic must stay
well tested and aligned with the shell loader behavior.

Mitigation:

- Add parser and payload-builder unit tests early.
- Keep the parsing rules explicit and local to one feature area.

## Why This Is the Safest Practical Choice

You said you are not a frontend developer. In that situation, the right stack is
usually not the most fashionable one. It is the stack with the clearest path to
a maintainable result and the lowest amount of accidental complexity.

This recommendation is conservative in the good sense:

- mature tools
- common patterns
- strong documentation
- large contributor pool
- low mismatch with the product shape
- predictable build behavior

It gives us a fast implementation path now without locking the project into an
unusual architecture later.

## Recommendation

Adopt the recommended stack and Docker-first build approach as the
implementation baseline for phase 1 and use them as the assumption for the PRD
and task breakdown.

---

Copyright (c) 2025-2026 Jeremy K. Johnson. All rights reserved.
