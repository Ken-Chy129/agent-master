package com.agentmaster.core

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.runBlocking
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SessionStoreTest {
    // --- Fakes so the store can be driven without a network. ---

    /** In-memory machine persistence. */
    private class FakeMachineStore(
        private var persisted: PersistedMachines = PersistedMachines(),
    ) : MachineStore {
        override suspend fun load(): PersistedMachines = persisted
        override suspend fun save(state: PersistedMachines) { persisted = state }
    }

    /** ApiClient whose network calls are inert so openSession stays hermetic. */
    private class FakeApiClient : ApiClient(ApiClientConfig(baseUrl = "http://test", token = "t")) {
        override suspend fun listSessions(limit: Int?, offset: Int?): ListSessionsResponse =
            ListSessionsResponse(sessions = emptyList(), hasMore = false)
    }

    /**
     * SseClient that records the options passed to [subscribe] so the test can
     * synchronously drive `onRender` / `onDelta` / `onReconnect` callbacks — the
     * same callbacks the real transport would fire.
     */
    private class FakeSseClient : SseClient(SseClientConfig(baseUrl = "http://test", token = "t")) {
        var captured: SseSubscribeOptions? = null
        var lastAfterSeq: Long = -1
        var unsubscribed = false

        override fun subscribe(
            sessionId: String,
            afterSeq: Long,
            opts: SseSubscribeOptions,
        ): SseSubscription {
            captured = opts
            lastAfterSeq = afterSeq
            return SseSubscription { unsubscribed = true }
        }
    }

    private class FakeFactory(
        val api: FakeApiClient = FakeApiClient(),
        val sse: FakeSseClient = FakeSseClient(),
    ) : ClientFactory {
        override fun api(machine: MachineProfile): ApiClient = api
        override fun sse(machine: MachineProfile): SseClient = sse
    }

    private fun store(factory: FakeFactory): SessionStore =
        SessionStore(
            scope = CoroutineScope(Dispatchers.Unconfined),
            machineStore = FakeMachineStore(),
            clientFactory = factory,
            idGenerator = { "test-id" },
        )

    /** Add a machine (which selects it) and open a session; returns the wired store. */
    private fun openedStore(factory: FakeFactory, sessionId: String = "s1"): SessionStore {
        val s = store(factory)
        runBlocking {
            s.addMachine(AddMachineInput(baseUrl = "http://test", token = "t"))
            s.openSession(sessionId)
        }
        return s
    }

    // --- render/delta wiring (mirrors store.ts openSession onRender/onDelta) ---

    @Test
    fun openSession_subscribesFromSeqZero() {
        val factory = FakeFactory()
        val s = openedStore(factory)
        // The initial am_render snapshot supplies rows; we always resume from 0.
        assertEquals(0L, factory.sse.lastAfterSeq)
        assertEquals("s1", s.state.value.currentSessionId)
        assertEquals(StreamStatus.CONNECTING, s.state.value.streamStatus) // before first frame
    }

    @Test
    fun onRender_setsRowsAndRunActive_running() {
        val factory = FakeFactory()
        val s = openedStore(factory)

        val snapshot = RenderState(
            basedOnSeq = 3,
            tailActivity = "running",
            rows = listOf(
                RenderRow(kind = "user", id = "u1", seq = 1, text = "hi"),
                RenderRow(kind = "tool", id = "tc_1", seq = 2, name = "Bash", status = "running"),
            ),
        )
        factory.sse.captured!!.onRender(snapshot)

        val st = s.state.value
        assertEquals(2, st.currentRows.size)
        assertEquals("hi", st.currentRows[0].text)
        assertEquals("Bash", st.currentRows[1].name)
        assertTrue(st.runActive) // tailActivity == "running"
        assertEquals(StreamStatus.OPEN, st.streamStatus)
        assertEquals(snapshot, st.currentRender)
    }

    @Test
    fun onRender_idleTail_clearsRunActive() {
        val factory = FakeFactory()
        val s = openedStore(factory)

        factory.sse.captured!!.onRender(
            RenderState(basedOnSeq = 4, tailActivity = "idle", lastRunState = "done"),
        )
        val st = s.state.value
        assertFalse(st.runActive)
        assertEquals("done", st.currentRender.lastRunState)
    }

    @Test
    fun onDelta_accumulatesStreamingText() {
        val factory = FakeFactory()
        val s = openedStore(factory)
        val opts = factory.sse.captured!!

        opts.onDelta(StreamDelta(runId = "r_1", text = "Hel", index = 0))
        opts.onDelta(StreamDelta(runId = "r_1", text = "lo", index = 1))

        assertEquals("Hello", s.state.value.streamingText)
    }

    @Test
    fun onRender_clearsStreamingText() {
        // A committed snapshot supersedes any in-flight token preview.
        val factory = FakeFactory()
        val s = openedStore(factory)
        val opts = factory.sse.captured!!

        opts.onDelta(StreamDelta(runId = "r_1", text = "partial…", index = 0))
        assertEquals("partial…", s.state.value.streamingText)

        opts.onRender(
            RenderState(
                basedOnSeq = 5,
                tailActivity = "idle",
                rows = listOf(RenderRow(kind = "assistant", id = "a5", seq = 5, text = "partial done")),
            ),
        )
        assertEquals("", s.state.value.streamingText) // cleared on snapshot
        assertEquals("partial done", s.state.value.currentRows.single().text)
    }

    @Test
    fun renderForOtherSession_isIgnored() {
        val factory = FakeFactory()
        val s = openedStore(factory, sessionId = "s1")
        // Capture s1's callbacks, then open a different session (re-subscribes).
        val staleOpts = factory.sse.captured!!
        runBlocking { s.openSession("s2") }

        // A late frame from the previous subscription must not clobber s2.
        staleOpts.onRender(
            RenderState(
                basedOnSeq = 9,
                tailActivity = "running",
                rows = listOf(RenderRow(kind = "user", id = "u9", seq = 9, text = "stale")),
            ),
        )
        val st = s.state.value
        assertEquals("s2", st.currentSessionId)
        assertTrue(st.currentRows.isEmpty()) // s2 got no snapshot; stale s1 frame ignored
    }

    @Test
    fun closeSession_resetsRenderState() {
        val factory = FakeFactory()
        val s = openedStore(factory)
        factory.sse.captured!!.onDelta(StreamDelta(runId = "r", text = "x", index = 0))

        s.closeSession()
        val st = s.state.value
        assertEquals(null, st.currentSessionId)
        assertFalse(st.runActive)
        assertEquals("", st.streamingText)
        assertEquals(StreamStatus.IDLE, st.streamStatus)
    }

    // --- errText (mirrors store.ts errText) ---

    @Test
    fun errText_apiError_includesStatus() {
        val e = ApiError(409, "run active", "http://x/send", "{}")
        assertEquals("run active (HTTP 409)", SessionStore.errText(e))
    }

    @Test
    fun errText_plainException() {
        assertEquals("boom", SessionStore.errText(RuntimeException("boom")))
    }
}
