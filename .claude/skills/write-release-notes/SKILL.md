---
name: write-release-notes
description: Prepare the next Flux release and write its user-facing What's New entry. Use when the user wants to cut a release, draft the in-app release notes, or populate the WhatsNewCatalogue for a new version. Also triggers on phrases like "write release notes", "draft what's new", "do the next release".
---

# Write Release Notes

End-to-end flow for shipping a new Flux release: bump the version, prep the engineering changelog, then translate that into the user-facing What's New sheet that ships in the app.

## Inputs

- `{input}`: optional explicit version (e.g. `1.2`, `2.0`). When absent, derive the next version by incrementing the current `MARKETING_VERSION` from `Flux/Flux.xcodeproj/project.pbxproj` by `0.1` (e.g. `1.1` → `1.2`, `1.9` → `2.0` if the user requests a major bump, otherwise `1.10`).

## Steps

### 1. Determine the target version

- If the user passed a version, use it verbatim.
- Otherwise, read `MARKETING_VERSION` from `Flux/Flux.xcodeproj/project.pbxproj` and increment the minor component by 1 (`1.1` → `1.2`).
- State the chosen version to the user before continuing.

### 2. Run the release-prep skill

Invoke the `release-prep` skill with the target version. Its job is to:
- Run tests and lint.
- Bump `MARKETING_VERSION` everywhere it appears.
- Condense the `[Unreleased]` block in `CHANGELOG.md` into a single dated section for the new version, focused on the net change since the last release.
- Save a long-form release notes file under `docs/release_notes/`.

Wait for `release-prep` to finish before continuing. Do not commit anything — `release-prep` is explicitly non-committing.

### 3. Extract candidate highlights from the cleaned changelog

- Read the new release section in `CHANGELOG.md` after `release-prep` has condensed it.
- Build a candidate list of items that belong in the in-app What's New sheet. Apply this filter:
  - **Include**: user-visible features, UI changes, behaviour changes a user would notice, new platforms, new pickers/screens, fixes to user-visible bugs.
  - **Exclude**: internal refactors, test-only changes, infrastructure tweaks invisible to end users, doc-only changes, dependency bumps without user impact, changelog/release-notes meta-changes, internal API renames.
- Group candidates by category: `new`, `improved`, `fixed`.
- Show the candidate list to the user in plain bullets, grouped by category, with a one-line rationale per item where the inclusion is non-obvious. Tell the user this is the working set before drafting copy.

### 4. Draft the user-facing release notes

Translate each kept candidate into a `Highlight` entry:

- `category`: `.new` for first-time features, `.improved` for changes to existing features, `.fixed` for bug fixes.
- `title`: 2–5 words, sentence case (e.g. "Light and dark mode", not "Implement Light/Dark Mode Toggle").
- `detail`: one sentence, plain language, explains *what it does for the user*. No ticket IDs, file paths, function names, or implementation jargon. If a feature has no user-meaningful detail beyond its title, omit `detail` (the field is optional).
- Order within each category by user impact, not changelog order.

Match the voice of the existing `1.1` entry in `Flux/Packages/FluxCore/Sources/FluxCore/WhatsNew/WhatsNewCatalogue.swift`. Keep the whole sheet scannable — aim for 4–8 highlights total. If the changelog has more, drop the smallest ones rather than ship a wall of text.

### 5. Offer for confirmation

Present the drafted highlights to the user as the Swift literal that would be inserted, with the version, date, and grouped highlights formatted exactly as they will appear in the catalogue. Ask explicitly whether to proceed or revise. Do not edit the catalogue until the user confirms.

If the user requests changes, revise and re-confirm. Don't push back unless the user is asking for something that breaks the established voice (engineering jargon, ticket IDs, multi-sentence details).

### 6. Update the catalogue

Once confirmed, edit `Flux/Packages/FluxCore/Sources/FluxCore/WhatsNew/WhatsNewCatalogue.swift`:
- Prepend a new `WhatsNewRelease` to the `releases` array (newest first ordering is irrelevant for the coordinator, but matches the existing layout).
- Use `dateOf(year:month:)` with the actual release year/month.
- Use the version string in the form the user has chosen (`"1.2"`, not `"1.2.0"`).

Run `make ios-build` (or at minimum the Swift package's tests for `FluxCore`) to confirm the catalogue still compiles.

Leave the change uncommitted. The user owns commit, tag, and release publication.

## Voice and Audience Reminders

- The What's New sheet is for users, not developers. They are checking what changed in their battery monitoring app.
- Avoid: "T-1234", "refactored", "extracted", file names, function names, "implementation", "architecture".
- Prefer: "You can now", "Settings", "Dashboard", concrete user-facing names of screens and controls.
- The `detail` field should answer "what does this mean for me?" not "what did the engineer do?".

## What This Skill Does NOT Do

- Does not commit, tag, or push.
- Does not edit `CHANGELOG.md` directly — that is `release-prep`'s job.
- Does not file App Store Connect release notes — explicit non-goal of the What's New feature.
- Does not localise — current What's New is English-only by design (see `specs/whats-new/decision_log.md`).
