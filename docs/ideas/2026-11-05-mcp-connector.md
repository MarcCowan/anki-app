# Idea: MCP connector so Claude can add cards

Status: superseded by docs/specs/2026-11-08-mcp-connector-design.md.

## Goal

While learning something in a Claude chat (claude.ai on the web, desktop or phone), once we agree a set of
flashcards is final, Claude adds them to a deck through a remote MCP connector.

## Facts checked (2026-11-03, claude.com connector docs)

- Claude calls remote MCP servers **from Anthropic's cloud**, not from your device, so the endpoint has to
  be reachable from the public internet. Anthropic publishes the outbound range for MCP tool calls:
  `160.79.104.0/21`.
- Custom connectors work on every plan. The Free plan allows only one.
- Transport: Streamable HTTP. Legacy SSE also works but is deprecated.
- Tool calls time out after 240 s, and results are limited to about 150,000 characters.
- Auth options: none, OAuth, or static "request headers". Request headers are in limited beta and probably
  not available to us.
- **OAuth needs no third-party service.** The app is its own authorization server, and its sign-in step is
  the existing passphrase. Claude.ai identifies itself either through its published client identity
  (a Client ID Metadata Document that Anthropic hosts) or through Dynamic Client Registration. You don't
  register anywhere else.

## Decisions so far

- **Exposure:** Tailscale Funnel publishes only the `/mcp` path. The web UI stays private to the tailnet.
  The GCP firewall blocks all inbound traffic.
- **Same binary, same port.** `/mcp` gets its own middleware chain. All other routes are unchanged, and so
  is the CSP, which only affects browsers.

  | Check | Existing routes | `/mcp` |
  |---|---|---|
  | Host allowlist | yes | yes |
  | JSON content type, 1 MB cap | yes | yes |
  | Sec-Fetch-Site / Origin | must be same-origin | any `Origin` or `Sec-Fetch-Site` header → 403 (browsers always send them, Claude's servers don't) |
  | Auth | passphrase session cookie | OAuth access token |
  | Rate limit | login only | per client |

  Write this down as an explicit exception in the spec and CLAUDE.md so it isn't read as weakening the
  Origin rule.
- **Auth: OAuth first.** It needs:
  - protected-resource metadata (RFC 9728) and authorization-server metadata (RFC 8414);
  - an authorize page (passphrase plus a consent screen);
  - a token endpoint with PKCE;
  - refresh tokens and revocation;
  - client identification through Claude's published identity, or DCR as a fallback.

  The rough estimate is 1–2 days of careful test-driven work.

  **Fallback:** a 256-bit token in the URL path (`/mcp/<token>`). Store it only as a hash, compare in
  constant time, never log the path, and rotate it with an env var. Hide auth behind an interface either
  way.
- **Tools:** add-only, for example `list_decks` and `add_cards(deck, cards[])`. No edit or delete. The same
  validation as §6 of the spec (6,000 code points, normalisation).
- **No approval inbox.** Instead:
  - Rate limits, for example at most 50 cards per call and 200 per day.
  - Each MCP call's cards are tagged with a **batch id**, and a "delete this batch" action removes a bad
    batch without restoring a backup.
  - Backups (part of the server work) cover real disasters.
- Optional extra check: if Funnel passes through the caller's IP, also require `160.79.104.0/21`.
  Not yet confirmed that it does.

## Open questions

- Does Tailscale Funnel pass the client IP to the app (for example in `X-Forwarded-For`)?
- Does claude.ai's published client identity work with a single-user authorization server, or do we
  need DCR?
- Which tools beyond `list_decks` and `add_cards`, if any? (Probably none to start.)
