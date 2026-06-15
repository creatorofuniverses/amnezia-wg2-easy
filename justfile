# AmneziaWG Easy — task runner.
# Plain mode = AWG2 only. Proxy mode = AWG2 + obfuscation proxy sidecar.

set dotenv-load := true

# List recipes.
default:
    @just --list

# Plain AWG2 (no proxy).
up:
    docker compose up -d

# Stop the plain stack.
down:
    docker compose down

# AWG2 + obfuscation proxy (builds the proxy image).
up-proxy:
    docker compose -f docker-compose.proxy.yml up -d --build

# Stop the proxy stack.
down-proxy:
    docker compose -f docker-compose.proxy.yml down

# Follow logs (plain stack).
logs:
    docker compose logs -f

# Follow logs (proxy stack).
logs-proxy:
    docker compose -f docker-compose.proxy.yml logs -f

# Show container status for both stacks.
ps:
    docker compose ps
