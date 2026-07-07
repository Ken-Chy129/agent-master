package com.agentmaster.app

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material3.Surface
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle

/**
 * Single-activity Compose host. Launched normally, or via the
 * `agentmaster://pair?...` deep link (singleTask -> onNewIntent). Deep links are
 * forwarded to the ViewModel, which parses and pairs the machine in :core.
 */
class MainActivity : ComponentActivity() {

    private val viewModel: MainViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()

        // Cold start via deep link.
        handleIntent(intent)

        setContent {
            AgentMasterTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    val state by viewModel.state.collectAsStateWithLifecycle()
                    AgentMasterApp(
                        state = state,
                        modifier = Modifier,
                        onAddMachine = viewModel::addMachine,
                        onRemoveMachine = viewModel::removeMachine,
                        onSelectMachine = viewModel::selectMachine,
                        onRefreshSessions = viewModel::refreshSessions,
                        onCreateSession = viewModel::createSession,
                        onOpenSession = viewModel::openSession,
                        onCloseSession = viewModel::closeSession,
                        onSendMessage = viewModel::sendMessage,
                        onInterrupt = viewModel::interrupt,
                        onClearError = viewModel::clearError,
                    )
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(intent: Intent?) {
        if (intent?.action == Intent.ACTION_VIEW) {
            viewModel.handleDeepLink(intent.dataString)
        }
    }
}
