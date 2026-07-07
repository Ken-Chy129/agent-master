package com.agentmaster.app

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.agentmaster.core.EventType
import com.agentmaster.core.StoreState
import com.agentmaster.core.StreamStatus
import com.agentmaster.core.WireEvent
import com.agentmaster.core.asAssistantMessage
import com.agentmaster.core.asError
import com.agentmaster.core.asRunFinished
import com.agentmaster.core.asToolCall
import com.agentmaster.core.asToolResult
import com.agentmaster.core.asUserMessage

/**
 * Conversation view. DUMB renderer: it walks [StoreState.currentEvents] (already
 * deduped and sorted by seq in :core) and renders each WireEvent as a row. It
 * does NOT group, pair tool calls/results, or recompute run state — `runActive`
 * comes from the store's ledger reducer.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConversationScreen(
    state: StoreState,
    onBack: () -> Unit,
    onSendMessage: (String) -> Unit,
    onInterrupt: () -> Unit,
) {
    val events = state.currentEvents
    val listState = rememberLazyListState()

    // Auto-scroll to the newest event as the ledger grows.
    LaunchedEffect(events.size) {
        if (events.isNotEmpty()) listState.animateScrollToItem(events.size - 1)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Conversation", maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            text = streamLabel(state),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back")
                    }
                },
            )
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .imePadding(),
        ) {
            if (state.historyLoading) {
                LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
            }

            LazyColumn(
                state = listState,
                modifier = Modifier
                    .weight(1f)
                    .fillMaxWidth()
                    .padding(horizontal = 12.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                items(events, key = { it.seq }) { event ->
                    EventRow(event)
                }
            }

            Composer(
                runActive = state.runActive,
                onSend = onSendMessage,
                onInterrupt = onInterrupt,
            )
        }
    }
}

private fun streamLabel(state: StoreState): String {
    val status = when (state.streamStatus) {
        StreamStatus.IDLE -> "idle"
        StreamStatus.CONNECTING -> "connecting…"
        StreamStatus.OPEN -> "live"
        StreamStatus.ERROR -> "reconnecting…"
    }
    return if (state.runActive) "$status · running" else status
}

/** Render one ledger event by its type. One row per event; no grouping. */
@Composable
private fun EventRow(event: WireEvent) {
    when (event.type) {
        EventType.USER_MESSAGE ->
            Bubble(role = "You", text = event.asUserMessage()?.text.orEmpty(), fromUser = true)

        EventType.ASSISTANT_MESSAGE ->
            Bubble(role = "Assistant", text = event.asAssistantMessage()?.text.orEmpty(), fromUser = false)

        EventType.TOOL_CALL -> {
            val c = event.asToolCall()
            MetaRow("→ tool ${c?.name.orEmpty()} (${c?.id.orEmpty()})", mono = true)
        }

        EventType.TOOL_RESULT -> {
            val r = event.asToolResult()
            MetaRow("← result (${r?.id.orEmpty()})", mono = true)
        }

        EventType.RUN_STARTED -> MetaRow("run started", mono = false)

        EventType.RUN_FINISHED -> {
            val f = event.asRunFinished()
            MetaRow("run finished: ${f?.state?.wire.orEmpty()}", mono = false)
        }

        EventType.ERROR -> Bubble(
            role = "Error",
            text = event.asError()?.message.orEmpty(),
            fromUser = false,
            error = true,
        )

        EventType.UNKNOWN -> MetaRow("(unsupported event: ${event.type})", mono = false)
    }
}

@Composable
private fun Bubble(role: String, text: String, fromUser: Boolean, error: Boolean = false) {
    val bg = when {
        error -> MaterialTheme.colorScheme.errorContainer
        fromUser -> MaterialTheme.colorScheme.primaryContainer
        else -> MaterialTheme.colorScheme.surfaceVariant
    }
    val fg = when {
        error -> MaterialTheme.colorScheme.onErrorContainer
        fromUser -> MaterialTheme.colorScheme.onPrimaryContainer
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    Row(
        modifier = Modifier.fillMaxWidth(),
        horizontalArrangement = if (fromUser) Arrangement.End else Arrangement.Start,
    ) {
        Surface(
            color = bg,
            shape = RoundedCornerShape(12.dp),
            modifier = Modifier.fillMaxWidth(0.92f),
        ) {
            Column(Modifier.padding(10.dp)) {
                Text(role, style = MaterialTheme.typography.labelSmall, color = fg)
                Spacer(Modifier.width(2.dp))
                Text(text, style = MaterialTheme.typography.bodyMedium, color = fg)
            }
        }
    }
}

@Composable
private fun MetaRow(text: String, mono: Boolean) {
    Text(
        text = text,
        style = MaterialTheme.typography.labelMedium.let {
            if (mono) it.copy(fontFamily = FontFamily.Monospace) else it
        },
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(horizontal = 4.dp, vertical = 2.dp),
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Composer(
    runActive: Boolean,
    onSend: (String) -> Unit,
    onInterrupt: () -> Unit,
) {
    var draft by remember { mutableStateOf("") }
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surface)
            .padding(8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        OutlinedTextField(
            value = draft,
            onValueChange = { draft = it },
            modifier = Modifier.weight(1f),
            placeholder = { Text("Message…") },
            maxLines = 5,
        )
        if (runActive) {
            IconButton(onClick = onInterrupt) {
                Icon(Icons.Filled.Stop, contentDescription = "Interrupt run")
            }
        } else {
            IconButton(
                enabled = draft.isNotBlank(),
                onClick = {
                    val text = draft.trim()
                    if (text.isNotEmpty()) {
                        onSend(text)
                        draft = ""
                    }
                },
            ) {
                Icon(Icons.AutoMirrored.Filled.Send, contentDescription = "Send")
            }
        }
    }
}
