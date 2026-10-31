# Lahore Ride Hailing

A single-repository ride-booking demo for Lahore. The planned stack is a Next.js and TypeScript rider/driver app, a Go and Chi API, and PostgreSQL as the source of truth.

## Project status

This repository is at the architecture and contract stage. It does not contain a runnable application yet. The first milestone records the requirements, API, data invariants, and implementation decisions so the frontend and backend can be built against one shared contract.

## Project guide

- [Requirements and acceptance map](docs/REQUIREMENTS.md)
- [API and domain contract](docs/CONTRACT.md)
- [Architecture and state diagrams](docs/ARCHITECTURE.md)
- [Architecture decisions](docs/DECISIONS.md)
- [OpenAPI draft](docs/openapi.yaml)

The contract describes a deliberately bounded demo: seeded Lahore locations and accounts, approximate fare and pickup estimates, cookie sessions, and transactional ride assignment. It does not claim real routing, live driver tracking, payments, or production dispatch.

## Next milestone

Build the monorepo and local development environment against these documents, then implement the database schema and seed data. Update this README with exact startup and verification commands as those capabilities exist.
