# Email Verifier API

A REST API server that verifies email addresses using the [AfterShip/email-verifier](https://github.com/AfterShip/email-verifier) package.

## Features

- Email verification endpoint
- Health check endpoint
- SMTP check enabled
- Gravatar check enabled

## Installation

1. Make sure you have Go installed (1.20 or later)
2. Clone this repository
3. Install dependencies:
   ```bash
   go mod download
   ```

## Running the Server

```bash
go run main.go
```

The server will start on port 8080.

## API Endpoints

### Health Check
```
GET /health
```

Response:
```json
{
    "status": "healthy",
    "time": "2023-XX-XX..."
}
```

### Verify Email
```
POST /verify
Content-Type: application/json

{
    "email": "example@domain.com"
}
```

Response:
```json
{
    "email": "example@domain.com",
    "result": {
        "format": true,
        "has_mx_record": true,
        "has_gravatar": false,
        "is_disposable": false,
        "is_role_account": false,
        "is_free": true,
        "is_deliverable": true,
        "message": ""
    },
    "timestamp": "2023-XX-XX..."
}
```

## Error Handling

The API returns appropriate HTTP status codes and error messages when something goes wrong:

- 400 Bad Request: Invalid input
- 500 Internal Server Error: Server-side errors

## License

MIT
