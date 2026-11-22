# Incidents

One blameless write-up for each outage or data scare: what happened, how we found out, why, and what changes so it
doesn't happen again. The point is the system, not who pressed what.

- **File name:** `YYYY-MM-DD-<slug>.md`, dated by when the incident **started** (not when it was found).
- **Severity:**
  - **SEV-1:** data loss or a security exposure.
  - **SEV-2:** full outage.
  - **SEV-3:** partial or degraded (one feature, connector only, slow).
- **Times in UTC.**
- **Keep the record honest.** Don't rewrite an incident's history afterwards. Update its action-item statuses as
  they close, and link the PR or commit that closed each one.
- **Never include secrets,** project IDs, tailnet names, IPs or card content.

## Sections

1. A header table: status, severity, duration, how it was detected, what was affected, data loss.
2. Summary: a few sentences.
3. Timeline (UTC).
4. Root cause, and contributing factors. Say how sure you are, and what evidence would change your mind.
5. What went well, and what went badly.
6. Action items: a table with type (Detect / Mitigate / Prevent / Process / Cleanup) and status.

## Index

- [2026-11-17: VM down for 3.5 days after an automatic kernel update](2026-11-17-vm-kernel-ext4-crash.md), SEV-2
