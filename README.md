# UnitLog Backend

Backend for UnitLog Pool Basirih. It owns unit snapshots, immutable IN/OUT movement transactions, and integrations used by the Android satpam and web operations clients.

## Requirements

- Go 1.25+
- PostgreSQL
- Consul access to the environment-specific JSON configuration

## Configuration

The application securely fetches a flat JSON value from Consul and decodes it with Viper. Configure access through the deployment environment:

```text
CONSUL_HTTP_ADDR=https://consul.example.internal
CONSUL_CONFIG_KEY=dev/be/truckin-be-config
```

`CONSUL_CONFIG_KEY` must be `dev/be/truckin-be-config` for development or `prod/be/truckin-be-config` for production. The JSON value must include the PRD keys plus `postgres_dsn`, `auth_jwt_hs256_secret`, `auth_permissions_claim`, `auth_actor_id_claim`, and `auth_actor_name_claim`. Do not commit secret values.

HTTPS is required by default. For an internal Consul deployment that cannot use TLS, explicitly opt in to insecure HTTP:

```text
CONSUL_HTTP_ADDR=http://172.16.17.17:8500
CONSUL_CONFIG_KEY=dev/be/truckin-be-config
CONSUL_ALLOW_INSECURE_HTTP=true
```

This sends configuration secrets in plaintext across the network. Enable TLS instead whenever possible.

## Commands

```text
go test ./...
go build -o bin/server ./cmd/server
go build -o bin/migrate ./cmd/migrate
go run ./cmd/server
go run ./cmd/migrate up
```

## HTTP

`GET /health` returns `200` only after the service has loaded configuration and PostgreSQL remains reachable; it returns `503` when PostgreSQL is unavailable.

`POST /api/v1/movements` requires an `Idempotency-Key` UUID header and the Android permission. All API errors use `{ "error": { "code": "...", "message": "..." } }`. Operations endpoints require the web permission and use 1-based `page` and `pageSize` query parameters.

### Movement API

```http
POST /api/v1/movements
Authorization: Bearer <Auth Service JWT>
Idempotency-Key: <UUID reused for retries>
Content-Type: application/json
```

```json
{
  "noLambung": "SAMT223",
  "direction": "OUT",
  "outDestination": "FILLING_SHED_KUIN",
  "sppNumber": "SPP-001"
}
```

For `IN`, provide one `inCategory`: `WAITING_FOR_ASSIGNMENT`, `WAITING_FOR_DELIVERY_TO_CUSTOMER`, or `SERVICE_AND_REPAIR_MAINTENANCE`. For `OUT`, use `BENGKEL_LUAR` with `workOrderNumber`, `FILLING_SHED_KUIN` or `CUSTOMER` with `sppNumber`, or `OTHER` with `driverId`, `driverName`, and `note`. The server owns transaction timestamps and document-derived driver/customer snapshots.

### Operations API

- `GET /api/v1/dashboard`
- `GET /api/v1/units?query=&status=&destination=&includeInactive=&page=&pageSize=`
- `GET /api/v1/units/export` with the same filters
- `GET /api/v1/units/{id}`
- `GET /api/v1/units/{id}/transactions?page=&pageSize=`
- `GET /api/v1/transactions?query=&direction=&detail=&from=&to=&page=&pageSize=`
- `GET /api/v1/transactions/export` with the same filters

Date filters use RFC3339 UTC timestamps. All list responses contain `data` and `pagination` (`page`, `pageSize`, `totalItems`).

### Swagger UI

After the service starts, open `http://localhost:8104/swagger/index.html` to inspect and call the API. Use the **Authorize** button to provide an Auth Service JWT as `Bearer <token>` for protected endpoints.

The generated definition is available at `/swagger/doc.json`. Regenerate the committed Swagger files after changing the HTTP contract:

```text
go install github.com/swaggo/swag/cmd/swag@v1.16.4
make swagger
```

## Delivery

`Dockerfile` builds both `/app/server` and `/app/migrate`. The image starts the server by default; run migrations with `docker run --entrypoint /app/migrate <image> up` after injecting the Consul environment variables.

`Jenkinsfile` accepts a `TARGET_ENV` parameter. It regenerates Swagger, builds the Docker `test` target, tags images with the selected environment, Jenkins build number, and source revision, and authenticates to the configured registry with the `truckin-registry` Jenkins credential. Development builds deploy on the Jenkins agent with rollback on health-check failure, then apply migrations. A migration failure stops the candidate and preserves the prior image for manual recovery because the database may be partially migrated. Production builds require manual approval and publish an image without deploying it.

The pipeline reads unauthenticated configuration from `http://172.16.17.17:8500` using `${TARGET_ENV}/be/truckin-be-config` and explicitly enables insecure HTTP. Its Consul values must set `server_port` to `8104`, which is the container's published and health-check port. Update the `DEV_CONSUL_HTTP_ADDR` and `PROD_CONSUL_HTTP_ADDR` values in `Jenkinsfile` if the deployment endpoints differ.

## References

- Gin quickstart: https://gin-gonic.com/en/docs/quickstart/
- sqlx: https://github.com/jmoiron/sqlx
- golang-migrate: https://github.com/golang-migrate/migrate
- JWT parser options: https://pkg.go.dev/github.com/golang-jwt/jwt/v5#ParserOption
- Viper configuration: https://github.com/spf13/viper
