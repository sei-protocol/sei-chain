# Sei Metrics

This is intended to be an utility library to expose additional metrics that's useful for monitoring a given Sei chain. It buils ontop of the existing [Cosmos SDK telemetry library](https://docs.cosmos.network/main/learn/advanced/telemetry)

Note: In-memory sink is always attached (when the telemetry is enabled) with 10 second interval and 1 minute retention. This means that metrics will be aggregated over 10 seconds, and metrics will be kept alive for 1 minute.
