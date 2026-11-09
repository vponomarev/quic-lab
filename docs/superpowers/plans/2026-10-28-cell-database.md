# Cell Database Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans inline; final fresh-context review.

**Goal:** Persistent multi-source cell estimates and history, background rescan prioritizing the trip.
**Architecture:** internal/cellgeo SQLite repository; existing Mylnikov resolver as worker adapter; bulk map lookup and durable backfill job.
**Tech Stack:** Go database/sql + modernc SQLite; existing vanilla JS.
**Spec:** docs/superpowers/specs/2026-10-28-cell-database.md

- [x] RED/GREEN repository tests: persistence, preference/history, negative results, errors preserving coordinates, priority/retry.
- [x] Worker/background scan and authenticated endpoints; test map reads without external requests.
- [x] UI uses bulk saved positions and polls queue status instead of N synchronous provider calls.
- [x] Linux tests/vet/build, final review, deploy with rollback; start persisted trip-first backfill and verify progress.

Review focus: crash recovery, DB errors surfaced (not empty success), no loss of existing coordinates on provider errors, no query on page refresh for known cells, bounded history scan/retention integration.

Completed and deployed. Evidence: docs/testing/cell-database-2026-10-28.md. Broader-history provider transfer explicitly approved by user. Background scan started and priority verified.
