package com.agentmaster.core

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertNull

class MachinesTest {
    @Test
    fun parsePairLink_valid() {
        val p = parsePairLink("agentmaster://pair?url=http://100.64.0.1:8888&token=secret123&name=Dev%20Box")
        assertEquals("http://100.64.0.1:8888", p?.url)
        assertEquals("secret123", p?.token)
        assertEquals("Dev Box", p?.name)
    }

    @Test
    fun parsePairLink_nameOptional() {
        val p = parsePairLink("agentmaster://pair?url=http://host:9000&token=t")
        assertEquals("http://host:9000", p?.url)
        assertEquals("t", p?.token)
        assertNull(p?.name)
    }

    @Test
    fun parsePairLink_missingUrl_isNull() {
        assertNull(parsePairLink("agentmaster://pair?token=t"))
    }

    @Test
    fun parsePairLink_missingToken_isNull() {
        assertNull(parsePairLink("agentmaster://pair?url=http://host"))
    }

    @Test
    fun parsePairLink_wrongScheme_isNull() {
        assertNull(parsePairLink("https://pair?url=http://host&token=t"))
    }

    @Test
    fun parsePairLink_garbage_isNull() {
        assertNull(parsePairLink("not a url"))
    }

    @Test
    fun defaultMachineName_hostAndPort() {
        assertEquals("100.64.0.1:8888", defaultMachineName("http://100.64.0.1:8888"))
    }

    @Test
    fun defaultMachineName_noPort() {
        assertEquals("example.com", defaultMachineName("https://example.com/api"))
    }

    @Test
    fun defaultMachineName_garbageFallsBackToInput() {
        assertEquals("nonsense", defaultMachineName("nonsense"))
    }
}
