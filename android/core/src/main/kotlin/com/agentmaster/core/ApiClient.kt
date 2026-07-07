package com.agentmaster.core

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.serialization.KSerializer
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * Typed error carrying the HTTP status and the server's `{ error }` message.
 * Thrown by every [ApiClient] method on a non-2xx response. Mirrors ApiError in
 * frontend/packages/core/src/api.ts.
 */
class ApiError(
    val status: Int,
    message: String,
    val url: String,
    /** Raw response body text (JSON or plain). */
    val body: String?,
) : Exception(message)

data class ApiClientConfig(
    /** Absolute daemon base URL, e.g. "http://localhost:8888". No trailing slash required. */
    val baseUrl: String,
    /** Bearer token for this machine. */
    val token: String,
    /** Optional OkHttp override (tests / custom timeouts). */
    val httpClient: OkHttpClient = defaultHttpClient(),
) {
    companion object {
        fun defaultHttpClient(): OkHttpClient = OkHttpClient()
    }
}

private fun trimTrailingSlash(s: String): String =
    if (s.endsWith("/")) s.trimEnd('/') else s

/**
 * REST client for the agent-master daemon. Every `/api/...` call sends
 * `Authorization: Bearer <token>`. On any non-2xx response an [ApiError] is
 * thrown carrying the status and the server's `{ error }` message.
 *
 * Mirrors ApiClient in frontend/packages/core/src/api.ts: same endpoints,
 * same auth, same error extraction, same empty-body handling.
 */
open class ApiClient(config: ApiClientConfig) {
    val baseUrl: String = trimTrailingSlash(config.baseUrl)
    private val token: String = config.token
    private val http: OkHttpClient = config.httpClient
    private val jsonMedia = "application/json".toMediaType()

    // --- endpoints ---

    /** GET /health (public, no auth). */
    suspend fun health(): HealthResponse =
        request("GET", "/health", HealthResponse.serializer(), auth = false)

    /** GET /api/info. */
    suspend fun info(): InfoResponse =
        request("GET", "/api/info", InfoResponse.serializer())

    /** GET /api/sessions?limit=&offset= */
    open suspend fun listSessions(limit: Int? = null, offset: Int? = null): ListSessionsResponse {
        val path = buildString {
            append("/api/sessions")
            val q = mutableListOf<String>()
            if (limit != null) q.add("limit=$limit")
            if (offset != null) q.add("offset=$offset")
            if (q.isNotEmpty()) append("?").append(q.joinToString("&"))
        }
        return request("GET", path, ListSessionsResponse.serializer())
    }

    /** POST /api/sessions */
    suspend fun createSession(body: CreateSessionRequest): Session =
        request(
            "POST",
            "/api/sessions",
            Session.serializer(),
            body = AmJson.encodeToString(CreateSessionRequest.serializer(), body),
        )

    /** GET /api/sessions/:id (404 -> ApiError). */
    suspend fun getSession(id: String): Session =
        request("GET", "/api/sessions/${encode(id)}", Session.serializer())

    /** DELETE /api/sessions/:id */
    suspend fun deleteSession(id: String): OkResponse =
        request("DELETE", "/api/sessions/${encode(id)}", OkResponse.serializer())

    /**
     * GET /api/sessions/:id/messages?before_seq=&limit=
     * Events are returned ascending by seq. Use the smallest returned seq as the
     * next `beforeSeq` to page backward.
     */
    suspend fun getMessages(
        id: String,
        beforeSeq: Long? = null,
        limit: Int? = null,
    ): ListMessagesResponse {
        val path = buildString {
            append("/api/sessions/").append(encode(id)).append("/messages")
            val q = mutableListOf<String>()
            if (beforeSeq != null) q.add("before_seq=$beforeSeq")
            if (limit != null) q.add("limit=$limit")
            if (q.isNotEmpty()) append("?").append(q.joinToString("&"))
        }
        return request("GET", path, ListMessagesResponse.serializer())
    }

    /**
     * POST /api/sessions/:id/send -> 202 { runId }.
     * Idempotent on `clientIntentId`. Throws ApiError(409) if a run is active.
     */
    suspend fun send(id: String, body: SendRequest): SendResponse =
        request(
            "POST",
            "/api/sessions/${encode(id)}/send",
            SendResponse.serializer(),
            body = AmJson.encodeToString(SendRequest.serializer(), body),
        )

    /** POST /api/sessions/:id/interrupt */
    suspend fun interrupt(id: String): OkResponse =
        request("POST", "/api/sessions/${encode(id)}/interrupt", OkResponse.serializer())

    // --- internals ---

    private suspend fun <T> request(
        method: String,
        path: String,
        serializer: KSerializer<T>,
        body: String? = null,
        auth: Boolean = true,
    ): T = withContext(Dispatchers.IO) {
        val url = "$baseUrl$path"
        val builder = Request.Builder().url(url.toHttpUrl())
        if (auth) builder.header("Authorization", "Bearer $token")

        val requestBody = body?.toRequestBody(jsonMedia)
        builder.method(method, requestBody)

        val response = http.newCall(builder.build()).await()
        val text = response.body?.string() ?: ""

        if (!response.isSuccessful) {
            val message = extractErrorMessage(text) ?: "HTTP ${response.code}"
            throw ApiError(response.code, message, url, text.ifEmpty { null })
        }

        // 204 / empty body -> decode an empty object.
        if (text.isEmpty()) {
            @Suppress("UNCHECKED_CAST")
            return@withContext AmJson.decodeFromString(serializer, "{}")
        }
        AmJson.decodeFromString(serializer, text)
    }

    private fun encode(s: String): String =
        java.net.URLEncoder.encode(s, "UTF-8").replace("+", "%20")

    private fun extractErrorMessage(text: String): String? =
        try {
            AmJson.decodeFromString(ErrorEnvelope.serializer(), text).error
        } catch (_: Exception) {
            null
        }
}

/** Bridge an OkHttp [Call] to a cancellable coroutine. */
private suspend fun Call.await(): Response =
    suspendCancellableCoroutine { cont ->
        enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                if (cont.isCancelled) return
                cont.resumeWithException(e)
            }

            override fun onResponse(call: Call, response: Response) {
                cont.resume(response)
            }
        })
        cont.invokeOnCancellation {
            try {
                cancel()
            } catch (_: Throwable) {
                // ignore
            }
        }
    }
