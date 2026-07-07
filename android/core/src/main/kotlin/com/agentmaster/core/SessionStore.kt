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
    val eventsBySession: Map<String, List<WireEvent>> = emptyMap(),
    val historyLoading: Boolean = false,
    val streamStatus: StreamStatus = StreamStatus.IDLE,
    val runActive: Boolean = false,
    val error: String? = null,
) {
    /** Events for the currently-open session, ascending by seq. */
    val currentEvents: List<WireEvent>
        get() = currentSessionId?.let { eventsBySession[it] } ?: emptyList()
}

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
 * - openSession loads history then SSE-subscribes from lastSeq;
 * - events deduped/sorted by seq (upsertEvent);
 * - runActive derived from the ledger (last run event wins).
 *
 * All UI (the Android ViewModel) is a dumb observer of [state]; it must not
 * recompute grouping or run state.
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
                        eventsBySession = emptyMap(),
                        streamStatus = StreamStatus.IDLE,
                        runActive = false,
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
                eventsBySession = emptyMap(),
                streamStatus = StreamStatus.IDLE,
                runActive = false,
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
     * Open a session: load the latest history page, then SSE-subscribe from the
     * last seq. Live events are deduped/sorted into the session's list and
     * runActive is recomputed from the ledger. Direct port of store.openSession.
     */
    suspend fun openSession(id: String) {
        val client = api ?: return
        val stream = sse ?: return

        stopStream()
        update {
            it.copy(
                currentSessionId = id,
                historyLoading = true,
                streamStatus = StreamStatus.CONNECTING,
                error = null,
            )
        }

        val history: List<WireEvent> = try {
            client.getMessages(id, limit = 200).events
        } catch (err: Throwable) {
            update { it.copy(historyLoading = false, streamStatus = StreamStatus.ERROR, error = errText(err)) }
            return
        }

        update {
            it.copy(
                historyLoading = false,
                eventsBySession = it.eventsBySession + (id to history),
                runActive = computeRunActive(history),
            )
        }

        val lastSeq = if (history.isNotEmpty()) history.last().seq else 0L
        activeSubscription = stream.subscribe(
            id,
            afterSeq = lastSeq,
            opts = object : SseSubscribeOptions {
                override fun onEvent(event: WireEvent) {
                    if (_state.value.currentSessionId != id) return
                    update { s ->
                        val list = upsertEvent(s.eventsBySession[id] ?: emptyList(), event)
                        s.copy(
                            eventsBySession = s.eventsBySession + (id to list),
                            runActive = computeRunActive(list),
                            streamStatus = StreamStatus.OPEN,
                        )
                    }
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
        /** Insert/replace an event by seq and keep the list sorted ascending. */
        fun upsertEvent(list: List<WireEvent>, event: WireEvent): List<WireEvent> {
            val idx = list.indexOfFirst { it.seq == event.seq }
            if (idx >= 0) {
                val next = list.toMutableList()
                next[idx] = event
                return next
            }
            return (list + event).sortedBy { it.seq }
        }

        /** Derive whether a run is active from the event list (last run event wins). */
        fun computeRunActive(events: List<WireEvent>): Boolean {
            for (i in events.indices.reversed()) {
                when (events[i].type) {
                    EventType.RUN_FINISHED -> return false
                    EventType.RUN_STARTED -> return true
                    else -> {}
                }
            }
            return false
        }

        /** Human-readable error text, matching store.ts errText. */
        fun errText(err: Throwable): String = when (err) {
            is ApiError -> "${err.message} (HTTP ${err.status})"
            else -> err.message ?: err.toString()
        }
    }
}
