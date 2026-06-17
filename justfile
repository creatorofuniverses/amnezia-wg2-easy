# AmneziaWG Easy — task runner. Single native image; IMITATE_PROTOCOL toggles imitation.

set dotenv-load := true

# List recipes.
default:
    @just --list

# Bring the stack up.
up:
    docker compose up -d --build

# Stop the stack.
down:
    docker compose down

# Follow logs.
logs:
    docker compose logs -f

# Show container status.
ps:
    docker compose ps
