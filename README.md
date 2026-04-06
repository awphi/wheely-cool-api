# wheely-cool-api

> ⚠️ **95% vibecoded.** This was written almost entirely by Claude. Proceed with appropriate scepticism.

A tiny Go HTTP API that fetches and caches bin collection schedules from Greater Cambridgeshire's Shared Waste ICS feed and serves them as JSON - designed to power a homepage widget showing the next collection day and which bins are going out.

## API

### `GET /collections/{house_id}`

```json
{
  "house_id": "100090139850",
  "next": {
    "date": "2026-04-09",
    "bins": [
      { "name": "Green Bin Collection", "color": "green" },
      { "name": "Blue Bin Collection", "color": "blue" }
    ]
  },
  "upcoming": [ ... ],
  "cached_at": "2026-04-05T16:26:18Z"
}
```

`next` is the immediately upcoming collection. `upcoming` is the full future list from the current ICS. Results are cached per `house_id` and refreshed after the last scheduled date has passed.

### `GET /health`

Returns `200 OK`.

## Running

```bash
docker run -p 8080:8080 ghcr.io/adamwph/wheely-cool-api:latest
```

Or locally:

```bash
go run .
```

`PORT` env var overrides the default `8080`.

## Homepage widget

A simple example integration for [homepage](https://gethomepage.dev/) is provided below:

Add to your `services.yaml`. The block display gives a compact three-field layout: relative date + one dot per bin type, remapped to emoji.

```yaml
- Bins:
    widget:
      type: customapi
      url: http://wheely-cool-api:8080/collections/YOUR_HOUSE_ID
      refreshInterval: 3600000 # 1 hour
      mappings:
        - field: next.date
          label: Next collection
          format: relativeDate
        - field: next.bins.0.color
          label: First bin
          format: text
          remap:
            - value: green
              to: "🟢 Green"
            - value: blue
              to: "🔵 Blue"
            - value: black
              to: "⚫ Black"
            - value: brown
              to: "🟤 Brown"
            - any: true
              to: "🗑️"
        - field: next.bins.1.color
          label: Second bin
          format: text
          remap:
            - value: green
              to: "🟢 Green"
            - value: blue
              to: "🔵 Blue"
            - value: black
              to: "⚫ Black"
            - value: brown
              to: "🟤 Brown"
            - any: true
              to: "🗑️"
```
