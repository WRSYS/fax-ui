# Telnyx Fax UI (Go)

A minimal web UI and HTTP wrapper around the Telnyx Fax API using the official Go SDK.

## Prerequisites
- Go 1.22+
- A Telnyx API Key with fax permissions

## Quick start

1) Set environment variables

```bash
export TELNYX_API_KEY="YOUR_TELNYX_API_KEY"
export PORT=8080 # optional
```

2) Run the server

```bash
go mod tidy
go run ./app
```

3) Open the UI
- http://localhost:${PORT:-8080}/ — send a fax
- http://localhost:${PORT:-8080}/faxes — list faxes
- http://localhost:${PORT:-8080}/fax?id={fax_id} — show a fax by ID

## Notes
- The form supports media via `media_url`. For production, consider uploading files to Telnyx Media and using `media_name`.
- The SDK handles retries and request options. You can add logging or timeouts as needed.
- Webhook handling is not included here. If you want, we can add a `/webhooks/telnyx` endpoint next and verify signatures.

## Docker

Build the image:

```bash
docker build -t fax-ui:dev .
```

Run the container (mount uploads for persistence):

```bash
docker run --rm -p 8080:8080 \
	-e TELNYX_API_KEY="YOUR_TELNYX_API_KEY" \
	-e FAX_CONNECTION_ID="conn_xxxxx" \
	-e FAX_FROM_DEFAULT="+15551234567" \
	-e HIPPA_MODE=false \
	-e PUBLIC_BASE_URL="http://localhost:8080" \
	-v "$PWD/uploads:/app/uploads" \
	fax-ui:dev
```

Flags can also be used instead of envs (example):

```bash
docker run --rm -p 8080:8080 \
	-e TELNYX_API_KEY="YOUR_TELNYX_API_KEY" \
	-v "$PWD/uploads:/app/uploads" \
	fax-ui:dev --connection_id=conn_xxxxx --from=+15551234567 --hipaa=false
```

Notes:
- PUBLIC_BASE_URL should be accessible by Telnyx when using file uploads (e.g., via an ngrok tunnel to your machine). If not set, it defaults to `http://localhost:${PORT}`.
- In HIPAA mode, Telnyx store flags are forced off. Uploads are allowed; ensure `PUBLIC_BASE_URL` is reachable by Telnyx if using file uploads.

To avoid persistent storage, you can keep uploaded files in memory:

```bash
docker run --rm -p 8080:8080 \
	-e TELNYX_API_KEY="YOUR_TELNYX_API_KEY" \
	-e UPLOAD_IN_MEMORY=true \
	fax-ui:dev
```

### Docker Compose with ngrok sidecar

```bash
export TELNYX_API_KEY="YOUR_TELNYX_API_KEY"
export NGROK_AUTHTOKEN="YOUR_NGROK_AUTHTOKEN"
docker compose up --build
```

This spins up:
- `app` on http://localhost:8080
- `ngrok` on http://localhost:4040 (inspect UI); the app will auto-detect and use the ngrok public URL for uploaded files via `NGROK_API_URL`.

## Versioning & Releases

- Conventional Commits drive semantic version bumps. Examples: `feat: add x`, `fix: correct y`, `feat!: breaking change`.
- Releases are managed by Release Please and will open a PR on `main`. Merging the PR creates a tag `vX.Y.Z` and updates the changelog.
- The Docker image is published to GHCR on tags as `ghcr.io/<owner>/fax-ui:X.Y.Z` and `latest`.
- The binary’s version is injected at build time (`-ldflags -X main.Version=…`). The Docker build passes the tag via `APP_VERSION` automatically.