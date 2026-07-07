package com.agentmaster.app

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val DarkColors = darkColorScheme(
    primary = Color(0xFF7CB7FF),
    onPrimary = Color(0xFF00305F),
    surface = Color(0xFF121316),
    onSurface = Color(0xFFE3E3E6),
    background = Color(0xFF0C0D0F),
    surfaceVariant = Color(0xFF1E2024),
)

private val LightColors = lightColorScheme(
    primary = Color(0xFF1A5FBF),
)

@Composable
fun AgentMasterTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    content: @Composable () -> Unit,
) {
    MaterialTheme(
        colorScheme = if (darkTheme) DarkColors else LightColors,
        content = content,
    )
}
