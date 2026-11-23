# Idea: prune GHCR images without giving CI Admin

Status: dropped 2026-11-23 after review; the repo keeps Admin on the package.

## Review outcome

After review with Marc on 2026-11-23, we concluded that this idea overstates the risk, and that **Write** is where
the real exposure lies, not Admin:

- **Only the `image` job can touch the package.** It is the only job with `packages: write`; the jobs that run npm
  and Go dependencies, the likeliest route for a compromise, get no package access at all. So "any workflow step"
  below is wrong: the reach is the five pinned actions in the `image` job.
- **Write is already the dangerous part.** A compromised action in that job could push a malicious image to `:main`,
  and the VM deploys it within a minute. Splitting pruning out leaves that untouched.
- **What Admin adds is minor.** Making the image public exposes nothing new: the source is public, and the image
  holds no data or secrets (those live on the VM). Deleting versions doesn't stop the VM, which keeps running the
  image it has pulled, and the next push rebuilds one.

A separate token and workflow would add more to look after than they protect. The `image` job uses Admin on every
deploy to prune old versions, which keeps the private package within the free storage allowance. The original
reasoning follows, as written.

## Why

The `image` job in `.github/workflows/main.yml` pushes the new image and then prunes old versions: untagged
leftovers, and everything past the newest 10. Pushing needs only **Write** on the `anki-app` package. Deleting
versions from a workflow needs **Admin**, so the repo has Admin on the package.

Admin is more than the job needs most of the time. Any workflow step in the repo gets it through `GITHUB_TOKEN`, so
a compromised dependency or action running in CI could also change the package's visibility (publishing the
private image), delete the version the VM runs, or change who has access. Pruning still matters: a private
package's storage counts against the free allowance, and each deploy adds a full image.

## Options

- **Separate prune workflow with its own token.** The `image` job keeps Write. A small scheduled workflow prunes
  with a fine-grained token limited to deleting package versions, stored as an Actions secret. Narrower, but it's
  a second token to rotate, and that workflow still holds delete rights.
- **Prune from the VM or the laptop.** Nothing in CI can delete. Costs a token outside GitHub and something to
  remember or schedule.
- **Keep Admin.** The status quo, and what the old repo had implicitly. Simplest, and the exposure is limited by
  pinned action SHAs and the required PR checks.

## Open questions

- How much of the free storage do 10 versions use today?
- Does a scheduled prune workflow pull its token into a job that also runs third-party code?
