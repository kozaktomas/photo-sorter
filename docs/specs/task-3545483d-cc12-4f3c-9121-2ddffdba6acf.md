# Make Dev Script Port Configurable

The project already has a `dev.sh` that builds and starts the backend on port 8085.

## Requirements

- In `dev.sh`, change the port assignment to use an environment variable with a default fallback: `BACKEND_PORT="${PORT:-8085}"`
- Make sure the rest of the script uses `$BACKEND_PORT` everywhere (it likely already does, just update the initial assignment)
- No other changes needed