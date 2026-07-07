package com.agentmaster.app

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.agentmaster.core.MachineProfile
import com.agentmaster.core.RecentSession
import com.agentmaster.core.StoreState
import com.agentmaster.core.defaultMachineName

/**
 * Home: machine switcher across the top, recent-session list below, and FABs to
 * add a machine / create a session. Pure presentation over [StoreState].
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    state: StoreState,
    onAddMachine: (baseUrl: String, token: String, name: String?) -> Unit,
    onRemoveMachine: (id: String) -> Unit,
    onSelectMachine: (id: String) -> Unit,
    onRefreshSessions: () -> Unit,
    onCreateSession: (workspaceDir: String, model: String?, title: String?) -> Unit,
    onOpenSession: (id: String) -> Unit,
) {
    var showAddMachine by remember { mutableStateOf(false) }
    var showCreateSession by remember { mutableStateOf(false) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Agent Master") },
                actions = {
                    if (state.activeMachineId != null) {
                        IconButton(onClick = onRefreshSessions) {
                            Icon(Icons.Filled.Refresh, contentDescription = "Refresh sessions")
                        }
                    }
                },
            )
        },
        floatingActionButton = {
            Column(horizontalAlignment = Alignment.End) {
                if (state.activeMachineId != null) {
                    FloatingActionButton(onClick = { showCreateSession = true }) {
                        Icon(Icons.Filled.Add, contentDescription = "New session")
                    }
                }
            }
        },
    ) { padding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding)
                .padding(horizontal = 16.dp),
        ) {
            MachineSwitcher(
                machines = state.machines,
                activeMachineId = state.activeMachineId,
                onSelectMachine = onSelectMachine,
                onRemoveMachine = onRemoveMachine,
                onAddMachine = { showAddMachine = true },
            )

            Spacer(Modifier.height(8.dp))

            when {
                state.activeMachineId == null -> EmptyHint(
                    "No machine selected. Add a daemon by URL + token, or open an " +
                        "agentmaster://pair link.",
                )

                state.sessionsLoading && state.sessions.isEmpty() -> LoadingRow()

                state.sessions.isEmpty() -> EmptyHint("No sessions yet. Tap + to create one.")

                else -> SessionList(sessions = state.sessions, onOpenSession = onOpenSession)
            }
        }
    }

    if (showAddMachine) {
        AddMachineDialog(
            onDismiss = { showAddMachine = false },
            onConfirm = { url, token, name ->
                onAddMachine(url, token, name)
                showAddMachine = false
            },
        )
    }

    if (showCreateSession) {
        CreateSessionDialog(
            onDismiss = { showCreateSession = false },
            onConfirm = { workspaceDir, model, title ->
                onCreateSession(workspaceDir, model, title)
                showCreateSession = false
            },
        )
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun MachineSwitcher(
    machines: List<MachineProfile>,
    activeMachineId: String?,
    onSelectMachine: (String) -> Unit,
    onRemoveMachine: (String) -> Unit,
    onAddMachine: () -> Unit,
) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .horizontalScroll(rememberScrollState())
            .padding(vertical = 8.dp),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        machines.forEach { m ->
            FilterChip(
                selected = m.id == activeMachineId,
                onClick = { onSelectMachine(m.id) },
                label = { Text(m.name) },
            )
        }
        AssistChip(
            onClick = onAddMachine,
            label = { Text("Add machine") },
            leadingIcon = { Icon(Icons.Filled.Add, contentDescription = null) },
        )
    }
}

@Composable
private fun SessionList(
    sessions: List<RecentSession>,
    onOpenSession: (String) -> Unit,
) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(sessions, key = { it.id }) { session ->
            SessionCard(session = session, onClick = { onOpenSession(session.id) })
        }
    }
}

@Composable
private fun SessionCard(session: RecentSession, onClick: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
    ) {
        Column(Modifier.padding(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = session.title.ifBlank { session.id },
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                if (session.activeRunId != null) {
                    Spacer(Modifier.width(8.dp))
                    RunningDot()
                }
            }
            if (session.lastPreview.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(
                    text = session.lastPreview,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@Composable
private fun AddMachineDialog(
    onDismiss: () -> Unit,
    onConfirm: (baseUrl: String, token: String, name: String?) -> Unit,
) {
    var url by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Add machine") },
        text = {
            Column {
                LabeledField(label = "Daemon URL", value = url, placeholder = "http://100.x.x.x:8888") { url = it }
                Spacer(Modifier.height(8.dp))
                LabeledField(label = "Token", value = token, placeholder = "Bearer token") { token = it }
                Spacer(Modifier.height(8.dp))
                LabeledField(
                    label = "Name (optional)",
                    value = name,
                    placeholder = if (url.isNotBlank()) defaultMachineName(url) else "Dev box",
                ) { name = it }
            }
        },
        confirmButton = {
            TextButton(
                enabled = url.isNotBlank() && token.isNotBlank(),
                onClick = { onConfirm(url.trim(), token.trim(), name.trim().ifBlank { null }) },
            ) { Text("Add") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@Composable
private fun CreateSessionDialog(
    onDismiss: () -> Unit,
    onConfirm: (workspaceDir: String, model: String?, title: String?) -> Unit,
) {
    var workspaceDir by remember { mutableStateOf("") }
    var title by remember { mutableStateOf("") }
    var model by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("New session") },
        text = {
            Column {
                LabeledField(label = "Workspace dir", value = workspaceDir, placeholder = "/home/you/project") {
                    workspaceDir = it
                }
                Spacer(Modifier.height(8.dp))
                LabeledField(label = "Title (optional)", value = title, placeholder = "Untitled") { title = it }
                Spacer(Modifier.height(8.dp))
                LabeledField(label = "Model (optional)", value = model, placeholder = "provider default") {
                    model = it
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = workspaceDir.isNotBlank(),
                onClick = {
                    onConfirm(
                        workspaceDir.trim(),
                        model.trim().ifBlank { null },
                        title.trim().ifBlank { null },
                    )
                },
            ) { Text("Create") }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LabeledField(
    label: String,
    value: String,
    placeholder: String,
    onValueChange: (String) -> Unit,
) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        modifier = Modifier.fillMaxWidth(),
        label = { Text(label) },
        placeholder = { Text(placeholder) },
        singleLine = true,
    )
}

@Composable
private fun LoadingRow() {
    Row(
        modifier = Modifier.fillMaxWidth().padding(24.dp),
        horizontalArrangement = Arrangement.Center,
    ) {
        CircularProgressIndicator()
    }
}

@Composable
private fun EmptyHint(text: String) {
    Text(
        text = text,
        style = MaterialTheme.typography.bodyMedium,
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        modifier = Modifier.padding(vertical = 24.dp),
    )
}
