package com.agentmaster.core

import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference

/** Callbacks for a per-session SSE subscription. Mirrors SseSubscribeOptions. */
interface SseSubscribeOptions {
    /** Called for every parsed `am_event` frame (drives the resume cursor). */
    fun onEvent(event: WireEvent)

    /** Called for every `am_render` snapshot (the transcript to display). */
    fun onRender(state: RenderState) {}

    /** Called for every live `am_delta` frame (token-level preview; ephemeral). */
    fun onDelta(delta: StreamDelta) {}

    /** Called on transport errors (before an auto-reconnect is scheduled). */
    fun onError(error: Throwable) {}

    /** Called when a reconnect is (re)established, with the seq we resume from. */
    fun onReconnect(afterSeq: Long) {}
}

data class SseClientConfig(
    /** Absolute daemon base URL, e.g. "http://localhost:8888". */
    val baseUrl: String,
    /** Bearer token; sent via query string because SSE clients can't set headers easily. */
    val token: String,
    /**
     * OkHttp client used for the streaming connection. Must NOT have a short read
     * timeout — SSE holds the connection open. The default disables read timeout.
     */
    val httpClient: OkHttpClient = defaultStreamingClient(),
    /** Base delay for reconnect backoff, ms. Default 1000. */
    val reconnectBaseDelayMs: Long = 1000,
    /** Max delay for reconnect backoff, ms. Default 15000. */
    val reconnectMaxDelayMs: Long = 15000,
) {
    companion object {
        /** SSE needs an infinite read timeout so the long-lived stream isn't cut. */
        fun defaultStreamingClient(): OkHttpClient =
            OkHttpClient.Builder()
                .readTimeout(0, TimeUnit.MILLISECONDS)
                .build()
    }
}

private fun trimTrailingSlash(s: String): String =
    if (s.endsWith("/")) s.trimEnd('/') else s

/** Returned by [SseClient.subscribe]; closes the stream and cancels reconnects. */
fun interface SseSubscription {
    fun unsubscribe()
}

/**
 * SSE client for the per-session event stream.
 *
 * Connects to `${baseUrl}/api/sessions/:id/stream?token=..&after_seq=..`, listens
 * for the named `am_event` frames, and tracks the last seq so it can
 * auto-reconnect from `lastSeq` after a transport drop. Also honors the server's
 * named `reconnect` event ({ afterSeq }).
 *
 * Mirrors SseClient in frontend/packages/core/src/sse.ts one-to-one:
 * - resume with `after_seq`, deliver only seq > lastSeq (server enforces; client
 *   also advances lastSeq),
 * - exponential backoff min(base * 2^attempt, max), reset on open,
 * - `reconnect` event resets lastSeq to its `afterSeq` and immediately reconnects.
 */
open class SseClient(private val config: SseClientConfig) {
    private val baseUrl: String = trimTrailingSlash(config.baseUrl)
    private val token: String = config.token
    private val http: OkHttpClient = config.httpClient
    private val scheduler: ScheduledExecutorService =
        Executors.newSingleThreadScheduledExecutor { r ->
            Thread(r, "am-sse-reconnect").apply { isDaemon = true }
        }

    /**
     * Subscribe to a session's stream. Returns a handle whose [SseSubscription.unsubscribe]
     * closes the connection and cancels any pending reconnect.
     */
    open fun subscribe(
        sessionId: String,
        afterSeq: Long = 0,
        opts: SseSubscribeOptions,
    ): SseSubscription {
        val closed = AtomicBoolean(false)
        val lastSeq = AtomicLong(afterSeq)
        val attempt = AtomicInteger(0)
        val currentSource = AtomicReference<EventSource?>(null)

        fun buildUrl(after: Long): String {
            val url = "$baseUrl/api/sessions/${encode(sessionId)}/stream".toHttpUrl()
                .newBuilder()
                .addQueryParameter("token", token)
                .addQueryParameter("after_seq", after.toString())
                .build()
            return url.toString()
        }

        // Forward declaration so listener callbacks can trigger reconnects.
        lateinit var connect: (Long) -> Unit

        fun scheduleReconnect() {
            if (closed.get()) return
            val a = attempt.getAndIncrement()
            // Clamp the shift: base << a overflows a Long past ~62 bits and would
            // wrap to a tiny/negative delay, busy-looping. The max cap is reached
            // long before that anyway (base 1s hits the 15s cap at a=4).
            val delay = minOf(config.reconnectBaseDelayMs shl minOf(a, 20), config.reconnectMaxDelayMs)
            try {
                scheduler.schedule({ connect(lastSeq.get()) }, delay, TimeUnit.MILLISECONDS)
            } catch (_: Exception) {
                // scheduler shut down during teardown; ignore
            }
        }

        // OkHttp passes the owning EventSource into every callback, so a single
        // listener closes over the reconnect loop without any self-reference hack.
        val listener = object : EventSourceListener() {
            override fun onOpen(eventSource: EventSource, response: Response) {
                if (closed.get()) return
                attempt.set(0) // reset backoff on a successful open
                opts.onReconnect(lastSeq.get())
            }

            override fun onEvent(
                eventSource: EventSource,
                id: String?,
                type: String?,
                data: String,
            ) {
                if (closed.get()) return
                when (type) {
                    // Default SSE frames also arrive with type "message"; the
                    // server names ours `am_event`.
                    "am_event", "message", null -> {
                        val event = parseWireEvent(data) ?: return
                        if (event.seq > lastSeq.get()) lastSeq.set(event.seq)
                        opts.onEvent(event)
                    }
                    // Server-derived render snapshot (the transcript to display).
                    // Does NOT advance the resume cursor.
                    "am_render" -> {
                        val rs = parseRender(data) ?: return
                        opts.onRender(rs)
                    }
                    // Live-only token deltas: no seq, do not advance lastSeq /
                    // resume cursor.
                    "am_delta" -> {
                        val delta = parseDelta(data) ?: return
                        opts.onDelta(delta)
                    }
                    // Server-initiated resync after a dropped-subscriber overflow.
                    "reconnect" -> {
                        val target = parseReconnectSeq(data)
                        if (target != null) lastSeq.set(target)
                        eventSource.cancel()
                        currentSource.compareAndSet(eventSource, null)
                        if (!closed.get()) connect(lastSeq.get())
                    }
                    else -> { /* ignore unknown named events */ }
                }
            }

            override fun onClosed(eventSource: EventSource) {
                // Server closed the stream cleanly; treat like a transport drop
                // and reconnect (unless we asked to close).
                if (closed.get()) return
                currentSource.compareAndSet(eventSource, null)
                scheduleReconnect()
            }

            override fun onFailure(
                eventSource: EventSource,
                t: Throwable?,
                response: Response?,
            ) {
                if (closed.get()) return
                opts.onError(t ?: RuntimeException("SSE failure (HTTP ${response?.code})"))
                currentSource.compareAndSet(eventSource, null)
                scheduleReconnect()
            }
        }

        connect = fun(after: Long) {
            if (closed.get()) return
            val request = Request.Builder()
                .url(buildUrl(after))
                .header("Accept", "text/event-stream")
                // Belt-and-suspenders: some proxies honor the SSE resume header.
                .header("Last-Event-ID", after.toString())
                .build()
            val source = EventSources.createFactory(http).newEventSource(request, listener)
            currentSource.set(source)
        }

        connect(lastSeq.get())

        return SseSubscription {
            closed.set(true)
            currentSource.getAndSet(null)?.cancel()
        }
    }

    /**
     * Flow variant (DESIGN.md §11 "SseClient(Flow)"). Emits every parsed
     * `am_event`; completes when the collector cancels. Reconnect/error are
     * handled internally like [subscribe]; transport errors do not terminate the
     * flow (they trigger a reconnect), matching the resilient store behavior.
     */
    fun events(sessionId: String, afterSeq: Long = 0): Flow<WireEvent> = callbackFlow {
        val sub = subscribe(
            sessionId,
            afterSeq,
            object : SseSubscribeOptions {
                override fun onEvent(event: WireEvent) {
                    trySend(event)
                }
            },
        )
        awaitClose { sub.unsubscribe() }
    }

    private fun encode(s: String): String =
        java.net.URLEncoder.encode(s, "UTF-8").replace("+", "%20")

    companion object {
        internal fun parseWireEvent(data: String): WireEvent? =
            try {
                val event = AmJson.decodeFromString(WireEvent.serializer(), data)
                // Guard mirrors the TS check: needs a numeric seq and a type.
                event
            } catch (_: Exception) {
                null
            }

        internal fun parseReconnectSeq(data: String): Long? =
            try {
                AmJson.decodeFromString(ReconnectPayload.serializer(), data).afterSeq
            } catch (_: Exception) {
                null
            }

        internal fun parseRender(data: String): RenderState? =
            try {
                AmJson.decodeFromString(RenderState.serializer(), data)
            } catch (_: Exception) {
                null
            }

        internal fun parseDelta(data: String): StreamDelta? =
            try {
                AmJson.decodeFromString(StreamDelta.serializer(), data)
            } catch (_: Exception) {
                null
            }
    }
}

@kotlinx.serialization.Serializable
internal data class ReconnectPayload(val afterSeq: Long? = null)
