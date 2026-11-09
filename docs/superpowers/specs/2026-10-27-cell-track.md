# Cell track design

Approved: integrated admin map; implementation details delegated by user on 2026-10-27.

Add a map section to existing device radio history. Select local start/end datetime (default last 24 hours, maximum 7 days), load chronological samples, resolve unique supported serving cells through Mylnikov using only MCC/MNC/area/cell identifiers. No Wi-Fi identities, device IDs or observation times go to provider. Explicit Build button starts lookups; Stop cancels. Leaflet is vendored; OSM tiles load from browser only when map opens, with origin-only external referrer and attribution.

Show points, selected point accuracy circle, chronological line segments, slider, coverage counts and provider errors. Break segments at unresolved samples or gaps over 10 minutes; do not calculate road routes or speed. All provider positions are approximate and exact cell matching is unverified. Unsupported radio types stay unresolved. Keep existing authentication/CSRF; bound range to 7 days and records to 25000 (reject overflow rather than truncate). Resolver caches up to 10000 entries in memory with 24h positive/1h negative TTL, serializes external requests with 500ms minimum spacing, 8s timeout and 64KiB response limit. Restart clears cache. Transient errors are not negative results.
