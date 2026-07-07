package com.agentmaster.core

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

/**
 * Wire types for the agent-master daemon HTTP+SSE API (v1).
 *
 * These mirror docs/API.md and frontend/packages/core/src/types.ts EXACTLY.
 * This is a "dumb client": the daemon owns conversation structure; the client
 * renders the event list it is given and does not recompute grouping.
 *
 * JSON field names are the wire names verbatim (camelCase: workspaceDir,
 * lastPreview, runId, createdAt, seq, type, payload). Where a Kotlin idiom
 * would rename a field we pin it with @SerialName.
 */

/** Shared JSON codec. Lenient so unknown server fields don't break the client. */
val AmJson: Json = Json {
    ignoreUnknownKeys = true
    encodeDefaults = false
    explicitNulls = false
    isLenient = true
}

/** Full session record (POST /api/sessions, GET /api/sessions/:id). */
@Serializable
data class Session(
    val id: String,
    val title: String,
    val provider: String, // "claude"
    val model: String, // "" = provider default
    val workspaceDir: String,
    val createdAt: String, // RFC3339
    val updatedAt: String, // RFC3339
    val archived: Boolean = false,
)

/** List-projection row (GET /api/sessions). */
@Serializable
data class RecentSession(
    val id: String,
    val title: String,
    val lastPreview: String = "",
    val lastSeq: Long = 0,
    val activeRunId: String? = null, // present while a run is active
    val updatedAt: String,
)

/** Discriminant for a wire event. Unknown types decode to [UNKNOWN]. */
@Serializable(with = EventTypeSerializer::class)
enum class EventType(val wire: String) {
    USER_MESSAGE("user_message"),
    ASSISTANT_MESSAGE("assistant_message"),
    TOOL_CALL("tool_call"),
    TOOL_RESULT("tool_result"),
    RUN_STARTED("run_started"),
    RUN_FINISHED("run_finished"),
    ERROR("error"),

    /** Forward-compatible: any type string the client does not yet know. */
    UNKNOWN("unknown"),
    ;

    companion object {
        fun fromWire(wire: String): EventType =
            entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

/** Serializes [EventType] as its wire string; unknown strings -> [EventType.UNKNOWN]. */
object EventTypeSerializer : KSerializer<EventType> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("EventType", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: EventType) {
        encoder.encodeString(value.wire)
    }

    override fun deserialize(decoder: Decoder): EventType =
        EventType.fromWire(decoder.decodeString())
}

/**
 * A single ledger event streamed over SSE / returned by /messages.
 *
 * `payload` is kept as a raw [JsonElement] because its shape depends on `type`.
 * Narrow it with the typed accessors below when you know the event type. This
 * matches the TS core where payload is `unknown`.
 */
@Serializable
data class WireEvent(
    val seq: Long,
    val type: EventType,
    val runId: String? = null,
    val payload: JsonElement = JsonNull,
    val createdAt: String = "",
)

// --- Per-type payload shapes (payload is a JsonElement on WireEvent; decode
// with the typed helpers below when you know the event type). ---

@Serializable
data class UserMessagePayload(val text: String = "")

@Serializable
data class AssistantMessagePayload(val text: String = "")

@Serializable
data class ToolCallPayload(
    val name: String = "",
    val id: String = "",
    val input: JsonElement = JsonNull, // arbitrary tool input
)

@Serializable
data class ToolResultPayload(
    val id: String = "",
    val output: JsonElement = JsonNull, // arbitrary tool output
)

@Serializable
data class RunStartedPayload(val runId: String = "")

/** Terminal run states. Unknown states decode to [RunState.UNKNOWN]. */
@Serializable(with = RunStateSerializer::class)
enum class RunState(val wire: String) {
    DONE("done"),
    INTERRUPTED("interrupted"),
    FAILED("failed"),
    UNKNOWN("unknown"),
    ;

    companion object {
        fun fromWire(wire: String): RunState =
            entries.firstOrNull { it.wire == wire } ?: UNKNOWN
    }
}

object RunStateSerializer : KSerializer<RunState> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("RunState", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: RunState) {
        encoder.encodeString(value.wire)
    }

    override fun deserialize(decoder: Decoder): RunState =
        RunState.fromWire(decoder.decodeString())
}

@Serializable
data class RunFinishedPayload(
    val runId: String = "",
    val state: RunState = RunState.UNKNOWN,
)

@Serializable
data class ErrorPayload(val message: String = "")

// --- Typed payload accessors mirroring EventPayloadMap in the TS core. ---

fun WireEvent.asUserMessage(): UserMessagePayload? =
    if (type == EventType.USER_MESSAGE) decode(UserMessagePayload.serializer()) else null

fun WireEvent.asAssistantMessage(): AssistantMessagePayload? =
    if (type == EventType.ASSISTANT_MESSAGE) decode(AssistantMessagePayload.serializer()) else null

fun WireEvent.asToolCall(): ToolCallPayload? =
    if (type == EventType.TOOL_CALL) decode(ToolCallPayload.serializer()) else null

fun WireEvent.asToolResult(): ToolResultPayload? =
    if (type == EventType.TOOL_RESULT) decode(ToolResultPayload.serializer()) else null

fun WireEvent.asRunStarted(): RunStartedPayload? =
    if (type == EventType.RUN_STARTED) decode(RunStartedPayload.serializer()) else null

fun WireEvent.asRunFinished(): RunFinishedPayload? =
    if (type == EventType.RUN_FINISHED) decode(RunFinishedPayload.serializer()) else null

fun WireEvent.asError(): ErrorPayload? =
    if (type == EventType.ERROR) decode(ErrorPayload.serializer()) else null

private fun <T> WireEvent.decode(serializer: KSerializer<T>): T? =
    try {
        AmJson.decodeFromJsonElement(serializer, payload)
    } catch (_: Exception) {
        null
    }

// --- REST response envelopes (mirror types.ts) ---

@Serializable
data class HealthResponse(val status: String = "", val version: String = "")

@Serializable
data class ProviderInfo(val available: Boolean = false, val path: String? = null)

/** GET /api/info. `providers` keyed by provider id, e.g. { claude: {...} }. */
@Serializable
data class InfoResponse(
    val name: String = "",
    val version: String = "",
    val providers: Map<String, ProviderInfo> = emptyMap(),
)

@Serializable
data class ListSessionsResponse(
    val sessions: List<RecentSession> = emptyList(),
    val hasMore: Boolean = false,
)

@Serializable
data class ListMessagesResponse(
    val events: List<WireEvent> = emptyList(),
    val hasMore: Boolean = false,
)

@Serializable
data class CreateSessionRequest(
    val workspaceDir: String,
    val model: String? = null,
    val title: String? = null,
)

@Serializable
data class SendRequest(
    val message: String,
    val clientIntentId: String? = null,
)

@Serializable
data class SendResponse(val runId: String = "")

@Serializable
data class OkResponse(val ok: Boolean = true)

/** Server error envelope: `{ "error": "<message>" }`. */
@Serializable
data class ErrorEnvelope(val error: String? = null)
