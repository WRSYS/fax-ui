# Contributing

Thanks for contributing! This project aims to keep releases simple and automated.

## Development

- Requirements: Go 1.22+, a Telnyx API key.
- Run locally:
  ```bash
  export TELNYX_API_KEY="YOUR_TELNYX_API_KEY"
  go run ./app
  ```
- Optional flags/envs: `--from`, `--connection_id`, `--hipaa`, `--public_base_url`, `--upload_dir`, `--upload_in_memory`.
- In-memory uploads: set `UPLOAD_IN_MEMORY=true` (no disk writes; files served from `/mem-uploads/{id}`).

## Testing

- Add Go tests under `app/*_test.go`.
- CI runs `go build` and `go test` if present.

## Conventional Commits

Follow Conventional Commits for meaningful changelogs and correct semver bumps:
- `feat:` new feature → minor bump
- `fix:` bug fix → patch bump
- `feat!:` or `fix!:` breaking change → major bump
- `chore:`, `docs:`, `refactor:`, `perf:`, `test:` as appropriate

## Release Flow (Automated)

1. Merge Conventional Commit PRs into `main`.
2. The Release Please workflow opens a Release PR.
3. Merge the Release PR → creates a Git tag `vX.Y.Z` and updates `CHANGELOG.md`.
4. The workflow also creates `vMAJOR` and `vMAJOR.MINOR` tags pointing to the release commit.
5. The Docker publish workflow builds and pushes:
   - `ghcr.io/<owner>/fax-ui:X.Y.Z`
   - `ghcr.io/<owner>/fax-ui:latest`
6. Your external Helm charts repo monitors GHCR and updates charts accordingly.

Notes:
- The app embeds its version via `-ldflags -X main.Version=X.Y.Z` during Docker builds. Source files are not modified for version bumps.

## Org/Repo Setup Required

- GitHub App authentication for Release Please:
  - Create a GitHub App (org-level recommended).
  - Permissions: Contents (Read & write), Pull requests (Read & write), Issues (Read & write).
  - Install the App on this repo.
  - Add secrets:
    - `APP_ID`: GitHub App ID
    - `APP_PRIVATE_KEY`: PEM contents of the App private key
- Repository settings:
  - Actions → General → Workflow permissions: Read and write.
  - Enable “Allow GitHub Actions to create and approve pull requests”.
- Branch protection: allow merges from PRs to `main`.

## Local Release Testing (Optional)

- You can simulate version injection without tagging:
  ```bash
  go build -ldflags "-X main.Version=0.0.0-test" -o fax-ui ./app
  ./fax-ui
  ```

## Docker

- Build locally:
  ```bash
  docker build --build-arg APP_VERSION=dev -t fax-ui:dev .
  ```
- Run:
  ```bash
  docker run --rm -p 8080:8080 \
    -e TELNYX_API_KEY="YOUR_TELNYX_API_KEY" \
    fax-ui:dev
  ```
