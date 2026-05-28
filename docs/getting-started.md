# Getting Started

## Prerequisites

- Go 1.21+

## Build

```bash
make build        # → bin/substrate
make client       # → bin/client
```

Or manually:

```bash
cd src && go build -o ../bin/substrate .
cd src && go build -o ../bin/client ./cmd/client
```

## Configure

Copy the example config and edit it with your LLM endpoint:

```bash
cp config.ini-example config.ini
# Edit config.ini: set [llm].endpoint, [llm].model, and paths
```

Key settings in `config.ini`:

| Section | Setting | Default | Purpose |
|---|---|---|---|
| `[server]` | `port` | `8080` | Webhook listener port |
| `[llm]` | `endpoint` | — | OpenAI-compatible API URL (**required**) |
| `[llm]` | `model` | — | Model name (**required**) |
| `[paths]` | `working_dir` | `./work/` | Sandbox root for file tools |
| `[paths]` | `state_dir` | `./state/` | Session persistence directory |
| `[tools.bash]` | `enabled` | `true` | Enable the bash tool |

## Run

```bash
./bin/substrate -config config.ini
```

The server starts listening on the configured host:port. Log output goes to the configured `log_dir`.

## Send a message (CLI client)

### Wait for response (default)

The client spins up a local callback server and prints the agent's response:

```bash
echo "Hello world" | ./bin/client -n "test-channel" "Explain yourself"
```

### Fire-and-forget

```bash
./bin/client -nc "Just send it, don't wait"
```

### Show reasoning and tool calls

```bash
./bin/client -t "Show me your thinking"
```

### Use an external callback URL

```bash
./bin/client -cb "https://your-server.com/webhook" "Send to my endpoint"
```

### Attach an image

```bash
./bin/client -v ./photo.png "What do you see in this image?"
```

## Test

```bash
make test       # all packages, with vet
make race       # all packages with -race
make fmt        # check formatting
```

Run a single test:

```bash
make test-one Test=TestSaveLoadRoundtrip
```

## Module paths

All Go packages are under `github.com/josefcub/substrate`:

| Local path | Import path |
|---|---|
| `src/` | `github.com/agent-project/harness` |
| `src/agent/` | `github.com/agent-project/harness/agent` |
| `src/tools/` | `github.com/agent-project/harness/tools` |
| `src/cmd/client/` | `github.com/agent-project/harness/cmd/client` |
