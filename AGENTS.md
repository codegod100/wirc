# AGENTS.md - WIRC Development Guide

## Build Commands
- `bun run dev` - Build CSS, WASM, and start web server on :8080
- `bun run build-css` - Build Tailwind CSS to static/output.css
- `bun run build-wasm` - Compile Go to WASM (frontend/main.go → static/main.wasm)
- `go run cmd/ircd/main.go` - Run IRC daemon on :8081 (persistent IRC connections)
- `go run cmd/webserver/main.go` - Run web server on :8080 (communicates with IRC daemon)
- `go run cmd/server/main.go` - Run legacy monolithic server (deprecated)

## Docker Commands
### Building Images Locally
- `docker build -f Dockerfile.ircd -t wirc-ircd .` - Build IRC daemon Docker image
- `docker build -f Dockerfile.webserver -t wirc-webserver .` - Build webserver Docker image
- `docker run -p 8081:8081 wirc-ircd` - Run IRC daemon in Docker container
- `docker run -p 8080:8080 wirc-webserver` - Run webserver in Docker container

### Using Docker Compose
- `docker-compose up` - Run both IRC daemon and webserver
- `docker-compose up -d` - Run both services in background
- `docker-compose up ircd` - Run only IRC daemon
- `docker-compose up webserver` - Run only webserver
- `docker-compose logs -f ircd` - View IRC daemon logs
- `docker-compose logs -f webserver` - View webserver logs
- `docker-compose down` - Stop and remove containers

### Using GHCR Images
- `docker run -p 8081:8081 ghcr.io/username/repo/ircd:latest` - Run IRC daemon from GHCR
- `docker run -p 8080:8080 ghcr.io/username/repo/webserver:latest` - Run webserver from GHCR

### Environment Variables
- **IRCD**: `PORT=8081` - IRC daemon port
- **Webserver**: 
  - `PORT=8080` - Web server port
  - `IRC_DAEMON_URL=http://localhost:8081` - URL to IRC daemon (use `http://ircd:8081` in docker-compose)

## Development Workflow
1. Start IRC daemon: `go run cmd/ircd/main.go` OR `docker-compose up -d ircd`
2. Start web server: `go run cmd/webserver/main.go`
3. Build frontend: `bun run build-css && bun run build-wasm`

## Docker Workflow
### Local Development with Docker
1. Build both services: `docker-compose build`
2. Start both services: `docker-compose up -d`
3. View logs: `docker-compose logs -f`

### Using GHCR Images
1. Pull images: `docker pull ghcr.io/username/repo/ircd:latest && docker pull ghcr.io/username/repo/webserver:latest`
2. Update docker-compose.yml to use GHCR images (see comments in file)
3. Start services: `docker-compose up -d`

## GitHub Actions
- **Auto-build**: Pushes to main/develop trigger Docker image builds
- **Multi-platform**: Images built for linux/amd64 and linux/arm64
- **GHCR Publishing**: Images published to GitHub Container Registry
- **Tagging**: 
  - `latest` for main branch
  - `v1.0.0` for version tags
  - Branch names for feature branches

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
- **IRC Daemon** (port 8081): Manages persistent IRC connections via HTTP API
- **Web Server** (port 8080): Serves static files and WebSocket interface
- **Communication**: HTTP REST API between web server and IRC daemon
- Go WASM frontend compiled to static/main.wasm
- Tailwind CSS for styling
- WebSocket communication between frontend and web server

## Important Notes
- NEVER start daemon processes without explicit timeouts
- ALWAYS use timeouts when running shell commands to prevent hanging
- IRC daemon runs on port 8081, web server on port 8080
- Static files served from ./static/ directory
- IRC connections persist when web server is restarted
- Environment variables allow configuration of ports and daemon URL
- Docker containers have health checks and automatic restart policies
- Use docker-compose for easier container management
- GitHub Actions automatically builds and publishes Docker images to GHCR
- Images support multi-platform (amd64/arm64) for broader compatibility
- Webserver Docker image includes built CSS and WASM frontend
- write git commits without mentioning claude