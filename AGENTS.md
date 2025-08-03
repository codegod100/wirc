# AGENTS.md - WIRC Development Guide

## Build Commands
- `bun run dev` - Build CSS, WASM, and start web server on :8080
- `bun run build-css` - Build Tailwind CSS to static/output.css
- `bun run build-wasm` - Compile Go to WASM (frontend/main.go → static/main.wasm)
- `go run cmd/server/main.go` - Run combined IRC client/web server on :8080

## Docker Commands
### Building Images Locally
- `docker build -t wirc-server .` - Build server Docker image
- `docker run -p 8080:8080 wirc-server` - Run server in Docker container

### Environment Variables
- `PORT=8080` - Server port (default: 8080)

## Development Workflow
1. Build frontend: `bun run build-css && bun run build-wasm`
2. Start server: `go run cmd/server/main.go`
3. Open browser to `http://localhost:8080`

## Docker Workflow
1. Build image: `docker build -t wirc-server .`
2. Run container: `docker run -p 8080:8080 wirc-server`

## Testing
- No test framework detected - use `go test ./...` for Go tests

## Code Style Guidelines
- **Go**: Standard Go formatting, use `gofmt`
- **TypeScript**: ESNext target, strict mode enabled, bundler module resolution
- **Imports**: Standard library first, then third-party, then local imports
- **Naming**: Go uses camelCase for private, PascalCase for public; TS uses camelCase
- **Types**: Use explicit types in Go structs; TS has strict type checking enabled
- **Error Handling**: Go uses explicit error returns; handle all errors appropriately

## Architecture
- **Combined Server** (port 8080): Single service that handles both IRC connections and web interface
- Go WASM frontend compiled to static/main.wasm
- Tailwind CSS for styling
- WebSocket communication between frontend and server for real-time messaging
- Direct IRC protocol connection to IRC servers

## Important Notes
- NEVER start daemon processes without explicit timeouts
- ALWAYS use timeouts when running shell commands to prevent hanging
- Server runs on port 8080 (configurable via PORT environment variable)
- Static files served from ./static/ directory
- Single service handles both IRC connections and web interface
- IRC connections are managed directly within the server process
- Docker image includes built CSS and WASM frontend
- write git commits without mentioning claude