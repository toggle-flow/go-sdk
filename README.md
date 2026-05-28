# go-sdk

Go SDK for [ToggleFlow](https://github.com/toggle-flow/ToggleFlow). Zero dependencies — stdlib only.

## Installation

```bash
go get github.com/toggle-flow/go-sdk
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    toggleflow "github.com/toggle-flow/go-sdk"
)

func main() {
    client := toggleflow.New(toggleflow.Options{
        SDKKey:  "sdk_your_key_here",
        BaseURL: "https://your-toggleflow-host",
    })

    if err := client.Init(context.Background()); err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    user := toggleflow.UserContext{
        Key: "user-123",
        Attributes: map[string]any{
            "email":   "user@example.com",
            "plan":    "pro",
            "country": "US",
        },
    }

    darkMode := client.GetBoolVariation("dark-mode", user, false)
    theme    := client.GetStringVariation("theme", user, "light")
    timeout  := client.GetNumberVariation("request-timeout", user, 5000)

    _ = darkMode
    _ = theme
    _ = timeout
}
```

## API

### `toggleflow.New(opts Options) *Client`

| Field | Type | Default | Description |
|---|---|---|---|
| `SDKKey` | `string` | — | SDK key from your environment settings |
| `BaseURL` | `string` | — | URL of your ToggleFlow instance |
| `PollInterval` | `int` | `30` | Seconds between flag re-fetches. Set to `0` for 30s default. |

### `client.Init(ctx context.Context) error`

Fetches all flags and starts the SSE stream and poll loop as background goroutines.
Returns an error only if the initial fetch fails.

### Variation methods

```go
client.IsEnabled(flagKey, user)                           // bool
client.GetBoolVariation(flagKey, user, fallback)          // bool
client.GetStringVariation(flagKey, user, fallback)        // string
client.GetNumberVariation(flagKey, user, fallback)        // float64
client.GetJSONVariation(flagKey, user, &target)           // error
client.GetVariation(flagKey, user)                        // json.RawMessage
```

The `fallback` is returned when the flag doesn't exist or hasn't loaded yet.

### `client.OnChange(flagKey string, fn func()) func()`

Register a callback that fires when a flag changes. Use `"*"` to listen for any change.
Returns an unsubscribe function.

```go
unsubscribe := client.OnChange("dark-mode", func() {
    fmt.Println("dark-mode changed")
})
defer unsubscribe()
```

### `client.Close()`

Cancels the background goroutines and waits for them to exit cleanly.

## How it works

On `Init`, the SDK fetches all flag configs in a single HTTP request and stores them in memory behind an `sync.RWMutex`. Two goroutines run in the background:

- **SSE goroutine** — holds an open connection to `/sdk/stream`. When a flag change event arrives, it immediately re-fetches the full flag list. Reconnects automatically with exponential backoff (1s → 30s) if the connection drops.
- **Poll goroutine** — re-fetches flags on a fixed interval as a fallback for environments where SSE is blocked by a proxy.

ETag support is built in — polling requests send `If-None-Match` and skip parsing when the server returns `304 Not Modified`.

Percentage rollouts use the same `sha256(flagKey + "." + userKey) % 100` hash as the server, ensuring consistent bucketing across all SDKs.
