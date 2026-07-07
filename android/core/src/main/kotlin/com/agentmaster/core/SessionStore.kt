package com.agentmaster.core

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import java.util.UUID

/** Coarse SSE connection status for the currently-open session (mirrors StreamStatus). */
enum class StreamStatus { IDLE, CONNECTING, OPEN, ERROR }

/**
 * The full client state, mirroring the `StoreState` shape in
 * frontend/apps/web/src/store.ts. Immutable snapshot; the store swaps a new one
 * on every change so Compose (or any observer) can diff it.
 */
data class StoreState(
    val initialized: Boolean = false,
    val machines: List<MachineProfile> = emptyList(),
    val activeMachineId: String? = null,
    val sessions: List<RecentSession> = emptyList(),
    val sessionsLoading: Boolean = false,
    val currentSessionId: String? = null,
    /** Server-derived transcript per session (we dumb-render this). */
    val renderBySession: Map<String, RenderState> = emptyMap(),
    val streamStatus: StreamStatus = StreamStatus.IDLE,
    val runActive: Boolean = false,
    /** Live token-preview text for the current run; cleared when a committed snapshot lands. */
    val streamingText: String = "",
    val error: String? = null,
) {
    /** Server-derived render snapshot for the currently-open session. */
    val currentRender: RenderState
        get() = currentSessionId?.let { renderBySession[it] } ?: EMPTY_RENDER

    /** Render rows for the currently-open session (dumb-rendered by the UI). */
    val currentRows: List<RenderRow>
        get() = currentRender.rows
}

/** An empty render snapshot for sessions we haven't received a frame for yet. */
val EMPTY_RENDER: RenderState = RenderState(basedOnSeq = 0, rows = emptyList(), tailActivity = "idle")

/** Input for [SessionStore.addMachine]; mirrors AddMachineInput. */
data class AddMachineInput(
    val name: String? = null,
    val baseUrl: String,
    val token: String,
)

/** Factory so tests can inject fakes for the API/SSE clients. */
interface ClientFactory {
    fun api(machine: MachineProfile): ApiClient
    fun sse(machine: MachineProfile): SseClient

    companion object {
        /** Real OkHttp-backed clients. */
        val Default: ClientFactory = object : ClientFactory {
            override fun api(machine: MachineProfile) =
                ApiClient(ApiClientConfig(baseUrl = machine.baseUrl, token = machine.token))

            override fun sse(machine: MachineProfile) =
                SseClient(SseClientConfig(baseUrl = machine.baseUrl, token = machine.token))
        }
    }
}

/**
 * Headless session/machine store. This is a direct port of the web Zustand store
 * in frontend/apps/web/src/store.ts:
 *
 * - machine list + active machine, persisted through [MachineStore];
 * - openSession just SSE-subscribes from seq 0; the stream's initial `am_render`
 *   snapshot supplies the rows (no separate history fetch);
 * - `renderBySession` holds the server-folded transcript snapshot per session;
 * - `runActive` comes straight from `tailActivity == "running"`;
 * - `streamingText` accumulates live `am_delta` fragments and is cleared whenever
 *   a committed render snapshot arrives.
 *
 * All UI (the Android ViewModel) is a dumb observer of [state]; it must not
 * recompute grouping, tool pairing, or run state.
 */
class SessionStore(
    private val scope: CoroutineScope,
    private val machineStore: MachineStore,
    private val clientFactory: ClientFactory = ClientFactory.Default,
    /** Generates client intent ids; overridable in tests. */
    private val idGenerator: () -> String = { UUID.randomUUID().toString() },
) {
    private val _state = MutableStateFlow(StoreState())
    val state: StateFlow<StoreState> = _state.asStateFlow()

    // Derived clients for the active machine.
    private var api: ApiClient? = null
    private var sse: SseClient? = null

    // Active SSE subscription (not part of the observable state).
    private var activeSubscription: SseSubscription? = null

    private fun update(transform: (StoreState) -> StoreState) {
        _state.value = transform(_state.value)
    }

    private fun makeClients(machine: MachineProfile?) {
        if (machine == null) {
            api = null
            sse = null
        } else {
            api = clientFactory.api(machine)
            sse = clientFactory.sse(machine)
        }
    }

    private fun findMachine(id: String?): MachineProfile? =
        _state.value.machines.firstOrNull { it.id == id }

    private fun stopStream() {
        activeSubscription?.unsubscribe()
        activeSubscription = null
    }

    // --- lifecycle / machines ---

    suspend fun init() {
        val persisted = machineStore.load()
        var activeId = persisted.activeId
        if (activeId != null && persisted.machines.none { it.id == activeId }) activeId = null
        if (activeId == null && persisted.machines.isNotEmpty()) activeId = persisted.machines.first().id

        makeClients(persisted.machines.firstOrNull { it.id == activeId })
        update {
            it.copy(
                initialized = true,
                machines = persisted.machines,
                activeMachineId = activeId,
            )
        }
        if (api != null) refreshSessions()
    }

    /** Add (or update, matched by baseUrl) a machine, then select it. */
    suspend fun addMachine(input: AddMachineInput) {
        val cleanUrl = input.baseUrl.trim().trimEnd('/')
        val token = input.token.trim()
        val name = input.name?.trim().takeUnless { it.isNullOrEmpty() } ?: defaultMachineName(cleanUrl)

        val existing = _state.value.machines.firstOrNull { it.baseUrl == cleanUrl }
        val id: String
        val machines: List<MachineProfile>
        if (existing != null) {
            id = existing.id
            machines = _state.value.machines.map {
                if (it.id == id) it.copy(name = name, token = token) else it
            }
        } else {
            id = idGenerator()
            machines = _state.value.machines + MachineProfile(id, name, cleanUrl, token)
        }

        update { it.copy(machines = machines) }
        machineStore.save(PersistedMachines(machines = machines, activeId = id))
        selectMachine(id)
    }

    /** Convenience for deep-link pairing (agentmaster://pair). */
    suspend fun addMachineFromPair(pair: PairPayload) {
        addMachine(AddMachineInput(name = pair.name, baseUrl = pair.url, token = pair.token))
    }

    suspend fun removeMachine(id: String) {
        val machines = _state.value.machines.filter { it.id != id }
        val wasActive = _state.value.activeMachineId == id
        var nextActive = _state.value.activeMachineId
        if (wasActive) nextActive = machines.firstOrNull()?.id

        update { it.copy(machines = machines) }
        machineStore.save(PersistedMachines(machines = machines, activeId = nextActive))

        if (wasActive) {
            if (nextActive != null) {
                selectMachine(nextActive)
            } else {
                stopStream()
                makeClients(null)
                update {
                    it.copy(
                        activeMachineId = null,
                        sessions = emptyList(),
                        currentSessionId = null,
                        renderBySession = emptyMap(),
                        streamStatus = StreamStatus.IDLE,
                        runActive = false,
                        streamingText = "",
                    )
                }
            }
        }
    }

    suspend fun selectMachine(id: String) {
        stopStream()
        makeClients(findMachine(id))
        machineStore.save(PersistedMachines(machines = _state.value.machines, activeId = id))
        update {
            it.copy(
                activeMachineId = id,
                sessions = emptyList(),
                currentSessionId = null,
                renderBySession = emptyMap(),
                streamStatus = StreamStatus.IDLE,
                runActive = false,
                streamingText = "",
                error = null,
            )
        }
        if (api != null) refreshSessions()
    }

    // --- sessions ---

    suspend fun refreshSessions() {
        val client = api ?: return
        update { it.copy(sessionsLoading = true) }
        try {
            val res = client.listSessions(limit = 100, offset = 0)
            update { it.copy(sessions = res.sessions, sessionsLoading = false) }
        } catch (err: Throwable) {
            update { it.copy(sessionsLoading = false, error = errText(err)) }
        }
    }

    suspend fun createSession(req: CreateSessionRequest) {
        val client = api ?: return
        try {
            val session = client.createSession(req)
            refreshSessions()
            openSession(session.id)
        } catch (err: Throwable) {
            update { it.copy(error = errText(err)) }
        }
    }

    /**
     * Open a session: SSE-subscribe from seq 0. The stream's initial `am_render`
     * snapshot provides the rows — no separate history fetch. `am_event` only
     * advances the SseClient resume cursor; `am_render` sets the render snapshot
     * + `runActive` and clears any in-flight `streamingText`; `am_delta` appends
     * live token preview. Direct port of store.openSession.
     */
    suspend fun openSession(id: String) {
        val stream = sse ?: return

        stopStream()
        update {
            it.copy(
                currentSessionId = id,
                streamStatus = StreamStatus.CONNECTING,
                streamingText = "",
                error = null,
            )
        }

        activeSubscription = stream.subscribe(
            id,
            afterSeq = 0,
            opts = object : SseSubscribeOptions {
                // `am_event` only advances the SseClient resume cursor; the
                // render snapshot is the transcript source.
                override fun onEvent(event: WireEvent) {}

                override fun onRender(state: RenderState) {
                    if (_state.value.currentSessionId != id) return
                    update { s ->
                        s.copy(
                            renderBySession = s.renderBySession + (id to state),
                            runActive = state.tailActivity == "running",
                            streamStatus = StreamStatus.OPEN,
                            // A committed snapshot supersedes any in-flight token preview.
                            streamingText = "",
                        )
                    }
                }

                override fun onDelta(delta: StreamDelta) {
                    if (_state.value.currentSessionId != id) return
                    update { it.copy(streamingText = it.streamingText + delta.text) }
                }

                override fun onError(error: Throwable) {
                    if (_state.value.currentSessionId == id) {
                        update { it.copy(streamStatus = StreamStatus.ERROR) }
                    }
                }

                override fun onReconnect(afterSeq: Long) {
                    if (_state.value.currentSessionId == id) {
                        update { it.copy(streamStatus = StreamStatus.OPEN) }
                    }
                }
            },
        )
    }

    fun closeSession() {
        stopStream()
        update {
            it.copy(
                currentSessionId = null,
                streamStatus = StreamStatus.IDLE,
                runActive = false,
                streamingText = "",
            )
        }
    }

    suspend fun sendMessage(text: String) {
        val client = api ?: return
        val sessionId = _state.value.currentSessionId ?: return
        val trimmed = text.trim()
        if (trimmed.isEmpty()) return
        try {
            client.send(sessionId, SendRequest(message = trimmed, clientIntentId = idGenerator()))
        } catch (err: Throwable) {
            update { it.copy(error = errText(err)) }
        }
    }

    suspend fun interrupt() {
        val client = api ?: return
        val sessionId = _state.value.currentSessionId ?: return
        try {
            client.interrupt(sessionId)
        } catch (err: Throwable) {
            update { it.copy(error = errText(err)) }
        }
    }

    fun clearError() {
        update { it.copy(error = null) }
    }

    /** Fire-and-forget helpers so UI callbacks don't need their own scope. */
    fun launchSendMessage(text: String) { scope.launch { sendMessage(text) } }
    fun launchInterrupt() { scope.launch { interrupt() } }
    fun launchOpenSession(id: String) { scope.launch { openSession(id) } }
    fun launchRefreshSessions() { scope.launch { refreshSessions() } }
    fun launchCreateSession(req: CreateSessionRequest) { scope.launch { createSession(req) } }
    fun launchSelectMachine(id: String) { scope.launch { selectMachine(id) } }
    fun launchAddMachine(input: AddMachineInput) { scope.launch { addMachine(input) } }
    fun launchRemoveMachine(id: String) { scope.launch { removeMachine(id) } }
    fun launchAddMachineFromPair(pair: PairPayload) { scope.launch { addMachineFromPair(pair) } }

    companion object {
        /** Human-readable error text, matching store.ts errText. */
        fun errText(err: Throwable): String = when (err) {
            is ApiError -> "${err.message} (HTTP ${err.status})"
            else -> err.message ?: err.toString()
        }
    }
}
