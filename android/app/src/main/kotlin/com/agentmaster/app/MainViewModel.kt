package com.agentmaster.app

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.agentmaster.core.AddMachineInput
import com.agentmaster.core.CreateSessionRequest
import com.agentmaster.core.PairPayload
import com.agentmaster.core.SessionStore
import com.agentmaster.core.StoreState
import com.agentmaster.core.parsePairLink
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch

/**
 * Thin Android ViewModel that owns a [SessionStore] (the headless port of the
 * web Zustand store in frontend/apps/web/src/store.ts) and exposes its state to
 * Compose. All conversation logic lives in :core; this class only:
 *   - injects the EncryptedSharedPreferences machine store,
 *   - runs init() once,
 *   - forwards UI actions and deep links to the store.
 *
 * It deliberately does NOT recompute transcript grouping or run state — the UI
 * dumb-renders the server `render_state` snapshot (`state.currentRows` +
 * `state.streamingText`), and `state.runActive` comes from the snapshot's
 * `tailActivity` in the store. `onRender`/`onDelta` are wired to the store's SSE
 * subscription inside `openSession`; the ViewModel only forwards UI actions.
 */
class MainViewModel(app: Application) : AndroidViewModel(app) {

    private val store = SessionStore(
        scope = viewModelScope,
        machineStore = EncryptedMachineStore(app),
    )

    /** Single observable state snapshot; Compose collects this. */
    val state: StateFlow<StoreState> = store.state

    init {
        viewModelScope.launch { store.init() }
    }

    // --- machines ---

    fun addMachine(baseUrl: String, token: String, name: String? = null) {
        store.launchAddMachine(AddMachineInput(name = name, baseUrl = baseUrl, token = token))
    }

    fun removeMachine(id: String) = store.launchRemoveMachine(id)

    fun selectMachine(id: String) = store.launchSelectMachine(id)

    // --- sessions ---

    fun refreshSessions() = store.launchRefreshSessions()

    fun createSession(workspaceDir: String, model: String? = null, title: String? = null) {
        store.launchCreateSession(
            CreateSessionRequest(
                workspaceDir = workspaceDir.trim(),
                model = model?.trim().takeUnless { it.isNullOrEmpty() },
                title = title?.trim().takeUnless { it.isNullOrEmpty() },
            ),
        )
    }

    fun openSession(id: String) = store.launchOpenSession(id)

    fun closeSession() = store.closeSession()

    fun sendMessage(text: String) = store.launchSendMessage(text)

    fun interrupt() = store.launchInterrupt()

    fun clearError() = store.clearError()

    // --- deep-link pairing (agentmaster://pair?url=&token=&name=) ---

    /** Handle a possible pairing deep link; no-op if it isn't one. */
    fun handleDeepLink(uri: String?) {
        val pair: PairPayload = uri?.let { parsePairLink(it) } ?: return
        store.launchAddMachineFromPair(pair)
    }
}
