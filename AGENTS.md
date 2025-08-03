# AGENTS.md - WIRC Development Guide

## Build Commands
- `bun run dev` - Build CSS, WASM, and start server on :8080
- `bun run build-css` - Build Tailwind CSS to static/output.css
- `bun run build-wasm` - Compile Go to WASM (frontend/main.go → static/main.wasm)
- `go run cmd/server/main.go` - Run server directly

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
- Go backend with WebSocket server (gorilla/websocket)
- Go WASM frontend compiled to static/main.wasm
- Tailwind CSS for styling
- WebSocket communication between frontend and backend for IRC proxy

## Important Notes
- NEVER start daemon processes without explicit timeouts
- ALWAYS use timeouts when running shell commands to prevent hanging
- Server runs on port 8080 by default
- Static files served from ./static/ directory