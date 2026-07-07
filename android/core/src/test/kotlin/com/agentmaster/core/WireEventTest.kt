package com.agentmaster.core

import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull
import kotlin.test.assertTrue

class WireEventTest {
    private fun decode(s: String) = AmJson.decodeFromString(WireEvent.serializer(), s)

    @Test
    fun assistantMessage_roundTrip() {
        val json =
            """{"seq":42,"type":"assistant_message","runId":"r_1","payload":{"text":"hi"},"createdAt":"2026-01-01T00:00:00Z"}"""
        val e = decode(json)
        assertEquals(42L, e.seq)
        assertEquals(EventType.ASSISTANT_MESSAGE, e.type)
        assertEquals("r_1", e.runId)
        assertEquals("hi", e.asAssistantMessage()?.text)

        // Re-encode and re-decode; the semantic fields survive.
        val reencoded = AmJson.encodeToString(WireEvent.serializer(), e)
        val e2 = decode(reencoded)
        assertEquals(e.seq, e2.seq)
        assertEquals(e.type, e2.type)
        assertEquals(e.runId, e2.runId)
        assertEquals("hi", e2.asAssistantMessage()?.text)
    }

    @Test
    fun userMessage_typedAccessor() {
        val e = decode("""{"seq":1,"type":"user_message","payload":{"text":"hello"},"createdAt":""}""")
        assertEquals("hello", e.asUserMessage()?.text)
        assertNull(e.asAssistantMessage()) // wrong-type accessor returns null
    }

    @Test
    fun toolCall_preservesArbitraryInput() {
        val json =
            """{"seq":5,"type":"tool_call","runId":"r_2","payload":{"name":"Read","id":"tc_1","input":{"file":"/x","n":3}},"createdAt":""}"""
        val e = decode(json)
        val call = e.asToolCall()
        assertEquals("Read", call?.name)
        assertEquals("tc_1", call?.id)
        assertEquals("/x", call?.input?.jsonObject?.get("file")?.jsonPrimitive?.content)
    }

    @Test
    fun toolResult_preservesArbitraryOutput() {
        val e = decode(
            """{"seq":6,"type":"tool_result","payload":{"id":"tc_1","output":{"ok":true}},"createdAt":""}""",
        )
        val res = e.asToolResult()
        assertEquals("tc_1", res?.id)
        assertTrue(res?.output?.jsonObject?.get("ok")?.jsonPrimitive?.content == "true")
    }

    @Test
    fun runStarted_and_runFinished() {
        val started = decode("""{"seq":10,"type":"run_started","payload":{"runId":"r_9"},"createdAt":""}""")
        assertEquals("r_9", started.asRunStarted()?.runId)

        val finished = decode(
            """{"seq":11,"type":"run_finished","payload":{"runId":"r_9","state":"done"},"createdAt":""}""",
        )
        assertEquals(RunState.DONE, finished.asRunFinished()?.state)

        val failed = decode(
            """{"seq":12,"type":"run_finished","payload":{"runId":"r_9","state":"failed"},"createdAt":""}""",
        )
        assertEquals(RunState.FAILED, failed.asRunFinished()?.state)
    }

    @Test
    fun errorEvent() {
        val e = decode("""{"seq":3,"type":"error","payload":{"message":"boom"},"createdAt":""}""")
        assertEquals("boom", e.asError()?.message)
    }

    @Test
    fun unknownType_forwardCompatible() {
        val e = decode("""{"seq":7,"type":"future_thing","payload":{},"createdAt":""}""")
        assertEquals(EventType.UNKNOWN, e.type)
    }

    @Test
    fun missingRunId_isNull() {
        val e = decode("""{"seq":1,"type":"user_message","payload":{"text":"x"},"createdAt":""}""")
        assertNull(e.runId)
    }

    @Test
    fun listMessagesResponse_decodes() {
        val json =
            """{"events":[{"seq":1,"type":"user_message","payload":{"text":"a"},"createdAt":""}],"hasMore":true}"""
        val res = AmJson.decodeFromString(ListMessagesResponse.serializer(), json)
        assertEquals(1, res.events.size)
        assertTrue(res.hasMore)
    }

    @Test
    fun recentSession_decodesWithOptionalActiveRun() {
        val withRun = AmJson.decodeFromString(
            RecentSession.serializer(),
            """{"id":"s1","title":"T","lastPreview":"p","lastSeq":9,"activeRunId":"r_1","updatedAt":"now"}""",
        )
        assertEquals("r_1", withRun.activeRunId)

        val noRun = AmJson.decodeFromString(
            RecentSession.serializer(),
            """{"id":"s2","title":"T2","lastPreview":"","lastSeq":0,"updatedAt":"now"}""",
        )
        assertNull(noRun.activeRunId)
    }

    @Test
    fun createSessionRequest_omitsNulls() {
        val body = AmJson.encodeToString(
            CreateSessionRequest.serializer(),
            CreateSessionRequest(workspaceDir = "/home/x"),
        )
        assertTrue(body.contains("\"workspaceDir\":\"/home/x\""))
        // explicitNulls=false -> model/title omitted when null.
        assertTrue(!body.contains("model"))
        assertTrue(!body.contains("title"))
    }
}
