package com.wang4386.cdtmonitor.widget

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Typography
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.sp

/**
 * Palette taken from the user's reference dashboard (a Clash-style client):
 * pale sage-grey ground, pure white cards with very large radii, one deep
 * green, and soft sage fills for tracks, chips and selected states.
 */
private val LightColors = lightColorScheme(
    primary = Color(0xFF1E5B4A),
    surface = Color(0xFFEDF1EC),
    surfaceVariant = Color(0xFFFFFFFF),
    onSurface = Color(0xFF15211B),
    onSurfaceVariant = Color(0xFF6E7F76),
    outlineVariant = Color(0xFFE1EAE2),
    error = Color(0xFFD93A2B),
)

private val DarkColors = darkColorScheme(
    primary = Color(0xFF5FBF9B),
    surface = Color(0xFF0C100E),
    surfaceVariant = Color(0xFF161D19),
    onSurface = Color(0xFFEFF4F0),
    onSurfaceVariant = Color(0xFF9DB0A5),
    outlineVariant = Color(0xFF232E28),
    error = Color(0xFFFF6B5E),
)

/** Soft sage fill: progress tracks, chips, selected nav pill. */
@Composable
fun sageFill(): Color =
    if (isSystemInDarkTheme()) Color(0xFF21302A) else Color(0xFFDCE9E0)

/** Mid sage used for the ring's remaining segment. */
@Composable
fun sageMid(): Color =
    if (isSystemInDarkTheme()) Color(0xFF3E5A4C) else Color(0xFF9DBEAD)

val CdtGreen = Color(0xFF1E8A4C)
val CdtOrange = Color(0xFFB56600)

private val CdtTypography = Typography(
    headlineMedium = TextStyle(
        fontSize = 26.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = (-0.3).sp,
    ),
    headlineLarge = TextStyle(
        fontSize = 32.sp,
        fontWeight = FontWeight.Bold,
        letterSpacing = (-0.4).sp,
    ),
    titleLarge = TextStyle(fontSize = 19.sp, fontWeight = FontWeight.SemiBold),
    titleMedium = TextStyle(fontSize = 16.sp, fontWeight = FontWeight.SemiBold),
    titleSmall = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.SemiBold),
    bodyLarge = TextStyle(fontSize = 16.sp),
    bodyMedium = TextStyle(fontSize = 14.sp),
    bodySmall = TextStyle(fontSize = 12.sp),
    labelLarge = TextStyle(fontSize = 14.sp, fontWeight = FontWeight.Medium),
    labelMedium = TextStyle(fontSize = 12.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.4.sp),
    labelSmall = TextStyle(fontSize = 11.sp, fontWeight = FontWeight.Medium, letterSpacing = 0.6.sp),
)

@Composable
fun CdtTheme(content: @Composable () -> Unit) {
    MaterialTheme(
        colorScheme = if (isSystemInDarkTheme()) DarkColors else LightColors,
        typography = CdtTypography,
        content = content,
    )
}
