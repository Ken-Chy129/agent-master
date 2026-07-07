package com.agentmaster.app

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import com.agentmaster.core.CreateSessionRequest
import com.agentmaster.core.StoreState

/**
 * Root composable. It is a DUMB renderer over the [StoreState] snapshot the
 * ViewModel provides — it does not derive transcript structure or run state.
 *
 * Navigation is simple local UI state: when a session is open in the store we
 * show the conversation screen; otherwise the home screen (machine switcher +
 * session list). This mirrors the web app's single-store, screen-per-state flow.
 */
@Composable
fun AgentMasterApp(
    state: StoreState,
    modifier: Modifier = Modifier,
    onAddMachine: (baseUrl: String, token: String, name: String?) -> Unit,
    onRemoveMachine: (id: String) -> Unit,
    onSelectMachine: (id: String) -> Unit,
    onRefreshSessions: () -> Unit,
    onCreateSession: (workspaceDir: String, model: String?, title: String?) -> Unit,
    onOpenSession: (id: String) -> Unit,
    onCloseSession: () -> Unit,
    onSendMessage: (text: String) -> Unit,
    onInterrupt: () -> Unit,
    onClearError: () -> Unit,
) {
    Box(modifier = modifier.fillMaxSize()) {
        if (state.currentSessionId != null) {
            ConversationScreen(
                state = state,
                onBack = onCloseSession,
                onSendMessage = onSendMessage,
                onInterrupt = onInterrupt,
            )
        } else {
            HomeScreen(
                state = state,
                onAddMachine = onAddMachine,
                onRemoveMachine = onRemoveMachine,
                onSelectMachine = onSelectMachine,
                onRefreshSessions = onRefreshSessions,
                onCreateSession = onCreateSession,
                onOpenSession = onOpenSession,
            )
        }

        // Global error toast/banner, dismissible; mirrors store.error surfacing.
        state.error?.let { message ->
            ErrorBanner(message = message, onDismiss = onClearError)
        }
    }
}
