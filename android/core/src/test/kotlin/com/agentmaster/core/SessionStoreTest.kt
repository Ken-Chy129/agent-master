package com.agentmaster.core

import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.put
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue

class SessionStoreTest {
    private fun ev(seq: Long, type: EventType): WireEvent =
        WireEvent(seq = seq, type = type, payload = buildJsonObject { put("k", "v") })

    // --- upsertEvent (mirrors store.ts upsertEvent) ---

    @Test
    fun upsert_appendsAndSortsBySeq() {
        var list = emptyList<WireEvent>()
        list = SessionStore.upsertEvent(list, ev(3, EventType.USER_MESSAGE))
        list = SessionStore.upsertEvent(list, ev(1, EventType.USER_MESSAGE))
        list = SessionStore.upsertEvent(list, ev(2, EventType.ASSISTANT_MESSAGE))
        assertEquals(listOf(1L, 2L, 3L), list.map { it.seq })
    }

    @Test
    fun upsert_dedupesBySeq_replaces() {
        var list = listOf(ev(1, EventType.USER_MESSAGE), ev(2, EventType.ASSISTANT_MESSAGE))
        // Same seq arrives again (SSE/history overlap) — replace, not append.
        list = SessionStore.upsertEvent(list, ev(2, EventType.ASSISTANT_MESSAGE))
        assertEquals(2, list.size)
        assertEquals(listOf(1L, 2L), list.map { it.seq })
    }

    // --- computeRunActive (mirrors store.ts computeRunActive) ---

    @Test
    fun runActive_true_afterRunStarted() {
        val events = listOf(
            ev(1, EventType.USER_MESSAGE),
            ev(2, EventType.RUN_STARTED),
            ev(3, EventType.ASSISTANT_MESSAGE),
        )
        assertTrue(SessionStore.computeRunActive(events))
    }

    @Test
    fun runActive_false_afterRunFinished() {
        val events = listOf(
            ev(2, EventType.RUN_STARTED),
            ev(3, EventType.ASSISTANT_MESSAGE),
            ev(4, EventType.RUN_FINISHED),
        )
        assertFalse(SessionStore.computeRunActive(events))
    }

    @Test
    fun runActive_false_whenNoRunEvents() {
        val events = listOf(ev(1, EventType.USER_MESSAGE))
        assertFalse(SessionStore.computeRunActive(events))
    }

    @Test
    fun runActive_lastRunEventWins() {
        // finished then a fresh started -> active again
        val events = listOf(
            ev(1, EventType.RUN_STARTED),
            ev(2, EventType.RUN_FINISHED),
            ev(3, EventType.RUN_STARTED),
        )
        assertTrue(SessionStore.computeRunActive(events))
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
