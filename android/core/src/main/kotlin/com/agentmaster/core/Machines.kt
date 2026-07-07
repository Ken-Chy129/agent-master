package com.agentmaster.core

import kotlinx.serialization.Serializable

/**
 * Multi-machine model. A client (web/desktop/mobile) holds a list of machine
 * profiles — each is one agent-master daemon — and connects to the active one.
 * There is no central hub; the client talks to each daemon directly.
 *
 * Mirrors frontend/packages/core/src/machines.ts.
 */

/** One paired daemon. `token` is a secret; store it securely on device. */
@Serializable
data class MachineProfile(
    val id: String,
    val name: String,
    val baseUrl: String,
    val token: String,
)

/** Persisted client state: the machine list plus which one is active. */
@Serializable
data class PersistedMachines(
    val machines: List<MachineProfile> = emptyList(),
    val activeId: String? = null,
)

/**
 * Storage adapter for the machine list. Web backs this with localStorage; the
 * Android app backs it with EncryptedSharedPreferences (see :app).
 */
interface MachineStore {
    suspend fun load(): PersistedMachines
    suspend fun save(state: PersistedMachines)
}

/** Deep-link pairing payload (`agentmaster://pair?url=&token=&name=`). */
data class PairPayload(
    val url: String,
    val token: String,
    val name: String? = null,
)

/** The storage key used for the machine list in either backend. */
const val MACHINES_STORAGE_KEY = "agent-master.machines"

/**
 * Parse an `agentmaster://pair?...` deep link into a [PairPayload], or null.
 *
 * Mirrors parsePairLink in machines.ts: requires the `agentmaster:` scheme and
 * both `url` and `token` query params; `name` is optional.
 */
fun parsePairLink(link: String): PairPayload? =
    try {
        val u = java.net.URI(link)
        // URI keeps the scheme without a trailing colon.
        if (u.scheme != "agentmaster") {
            null
        } else {
            val params = parseQuery(u.rawQuery)
            val url = params["url"]
            val token = params["token"]
            if (url.isNullOrEmpty() || token.isNullOrEmpty()) {
                null
            } else {
                PairPayload(url = url, token = token, name = params["name"])
            }
        }
    } catch (_: Exception) {
        null
    }

/** Best-effort human label for a machine from its base URL (host[:port]). */
fun defaultMachineName(baseUrl: String): String =
    try {
        val u = java.net.URI(baseUrl)
        val host = u.host ?: return baseUrl
        if (u.port != -1) "$host:${u.port}" else host
    } catch (_: Exception) {
        baseUrl
    }

/** Decode a raw query string into a param map with URL-decoded values. */
private fun parseQuery(rawQuery: String?): Map<String, String> {
    if (rawQuery.isNullOrEmpty()) return emptyMap()
    return rawQuery.split("&").mapNotNull { pair ->
        if (pair.isEmpty()) return@mapNotNull null
        val idx = pair.indexOf('=')
        if (idx < 0) {
            decode(pair) to ""
        } else {
            decode(pair.substring(0, idx)) to decode(pair.substring(idx + 1))
        }
    }.toMap()
}

private fun decode(s: String): String =
    try {
        java.net.URLDecoder.decode(s, "UTF-8")
    } catch (_: Exception) {
        s
    }
