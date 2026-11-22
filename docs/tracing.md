# Follow a message through Jaeger

Start the app with a local Jaeger collector and query UI. Run these commands from the repository root in Bash.

```bash
stack=(docker compose -f docker-compose.yml -f docker-compose.tracing.yml)
"${stack[@]}" up -d --build --wait
```

Open [EchoStream](http://localhost:5173), create a channel, and send a message. In the [Jaeger UI](http://localhost:16686), select the `echostream` service and search for the `POST /v1/channels/:id/messages` operation. Allow a few seconds for batched spans to arrive.

| Span | What happened |
| --- | --- |
| `POST /v1/channels/:id/messages` | The API authenticated the send and committed the message and its outbox row together. The row stores the request’s W3C trace context. |
| `outbox.publish` | A worker read the saved trace context and published the message event to Redis. A failed publish records an error on the attempt and leaves the row eligible for retry. |
| `redis.fanout` | Each API instance receiving the Redis event handed the message to its local WebSocket hub. The trace context is removed before forwarding the frame to the browser. |

To exercise delivery across two instances, start a second API process and run the smoke client with Node 22 or newer. This creates a disposable workspace and channel, sends through the second API, and verifies receipt through the original instance. The smoke also retries the send, so choose the successful `201` span when inspecting its trace. Expand the resource tags to distinguish the `peer` writer from the `primary` reader.

```bash
"${stack[@]}" run -d --no-deps --name echostream-trace-peer \
  -e OTEL_RESOURCE_ATTRIBUTES=service.instance.id=peer -p 127.0.0.1:8082:8081 api
curl --fail --retry 12 --retry-all-errors --retry-delay 1 http://127.0.0.1:8082/v1/ready
node scripts/smoke.mjs http://127.0.0.1:8082 http://127.0.0.1:5173
docker rm -f echostream-trace-peer
```

The overlay enables sampling for every request and stores traces in Jaeger’s memory. Stopping the stack clears these traces and retains PostgreSQL data in its named volume.

```bash
"${stack[@]}" down
```

[Jaeger all-in-one configuration](https://www.jaegertracing.io/docs/2.23/getting-started/)
