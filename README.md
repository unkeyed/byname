# Byname

Track how your brand shows up in AI answers and in Google Search Console, from one self-hosted container and your own Postgres.

## Run it locally with Docker

```sh
echo "ENCRYPTION_KEY=$(openssl rand -base64 32)" > .env
docker compose up -d --build --wait
docker compose logs app | grep -A2 'Open this link'
```

Open the setup link from the log to create the first account, an admin, with a passkey. Keep `ENCRYPTION_KEY`: without it, stored secrets cannot be read.

## Develop

```sh
make dev            # Postgres in Docker, the server on :8080
cd web && npm run dev   # the UI on :5173, calling the server
make test           # needs the compose Postgres: docker compose up -d db
```

The REST API is described at `/v1/openapi.yaml`. MCP clients connect to `/mcp` and sign in with OAuth.

## License

MIT
