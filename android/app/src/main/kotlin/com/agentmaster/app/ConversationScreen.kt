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
import com.agentmaster.core.RenderRow
import com.agentmaster.core.StoreState
import com.agentmaster.core.StreamStatus
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonPrimitive

/**
 * Conversation view. DUMB renderer: it walks the server-derived
 * [StoreState.currentRows] (the `am_render` snapshot folded server-side — tool
 * calls already paired with their results, run status already resolved) and
 * renders each [RenderRow] by `kind`. It does NOT group, pair tool
 * calls/results, or recompute run state — `runActive` comes from the snapshot's
 * `tailActivity`. A live `streamingText` preview (accumulated `am_delta`
 * fragments) is appended below the committed rows and disappears when the next
 * snapshot lands. Mirrors frontend/apps/web/src/components/Conversation.tsx.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConversationScreen(
    state: StoreState,
    onBack: () -> Unit,
    onSendMessage: (String) -> Unit,
    onInterrupt: () -> Unit,
) {
    val rows = state.currentRows
    val streamingText = state.streamingText
    val listState = rememberLazyListState()

    // Auto-scroll to the newest row and as the live preview grows.
    val itemCount = rows.size + if (streamingText.isNotEmpty()) 1 else 0
    LaunchedEffect(itemCount, streamingText) {
        if (itemCount > 0) listState.animateScrollToItem(itemCount - 1)
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
            // Connecting only reads as loading before the first snapshot arrives.
            if (state.streamStatus == StreamStatus.CONNECTING && rows.isEmpty()) {
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
                items(rows, key = { it.id }) { row ->
                    RenderRowView(row)
                }
                // Live token preview: ephemeral, replaced when the next snapshot lands.
                if (streamingText.isNotEmpty()) {
                    item(key = "__streaming__") {
                        Bubble(role = "Assistant", text = "$streamingText▌", fromUser = false)
                    }
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

/**
 * Dumb-renders one server-derived row by `kind`. Tool pairing/status is already
 * done server-side; we only display. Mirrors the web `Row` component.
 */
@Composable
private fun RenderRowView(row: RenderRow) {
    when (row.kind) {
        "user" -> Bubble(role = "You", text = row.text.orEmpty(), fromUser = true)

        "assistant" -> Bubble(role = "Assistant", text = row.text.orEmpty(), fromUser = false)

        "tool" -> {
            val header = buildString {
                append("→ tool ").append(row.name.orEmpty())
                append(if (row.status == "done") " · done" else " · running…")
            }
            MetaRow(header, mono = true)
            val input = formatValue(row.input)
            if (input.isNotEmpty()) MetaRow(input, mono = true)
            val output = formatValue(row.output)
            if (output.isNotEmpty()) MetaRow("→ $output", mono = true)
        }

        "error" -> Bubble(role = "Error", text = row.text.orEmpty(), fromUser = false, error = true)

        else -> MetaRow("(unsupported row: ${row.kind})", mono = false)
    }
}

/** Renders arbitrary tool input/output JSON for display; "" when absent. */
private fun formatValue(v: JsonElement?): String = when {
    v == null || v is JsonNull -> ""
    v is JsonPrimitive && v.isString -> v.content
    else -> v.toString()
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
