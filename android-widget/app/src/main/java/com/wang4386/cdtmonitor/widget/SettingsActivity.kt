package com.wang4386.cdtmonitor.widget

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBars
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Tune
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp

/**
 * App shell: two tabs with a floating pill navigation bar, matching the
 * user's reference client (white floating bar, sage selected pill).
 * Each screen carries its own in-content header.
 */
class SettingsActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent {
            CdtTheme {
                AppShell()
            }
        }
    }
}

@Composable
private fun AppShell() {
    var tab by remember { mutableIntStateOf(0) }
    Scaffold(
        bottomBar = {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .windowInsetsPadding(WindowInsets.navigationBars)
                    .padding(horizontal = 18.dp, vertical = 10.dp),
            ) {
                Surface(
                    shape = RoundedCornerShape(999.dp),
                    color = MaterialTheme.colorScheme.surfaceVariant,
                    shadowElevation = 6.dp,
                ) {
                    Row(modifier = Modifier.padding(6.dp)) {
                        FloatingNavItem(
                            icon = Icons.Filled.Dashboard,
                            label = "总览",
                            selected = tab == 0,
                            modifier = Modifier.weight(1f),
                        ) { tab = 0 }
                        FloatingNavItem(
                            icon = Icons.Filled.Tune,
                            label = "管理",
                            selected = tab == 1,
                            modifier = Modifier.weight(1f),
                        ) { tab = 1 }
                        FloatingNavItem(
                            icon = Icons.Filled.Settings,
                            label = "设置",
                            selected = tab == 2,
                            modifier = Modifier.weight(1f),
                        ) { tab = 2 }
                    }
                }
            }
        },
    ) { innerPadding ->
        Box(modifier = Modifier.padding(innerPadding)) {
            when (tab) {
                0 -> DashboardScreen(onOpenSettings = { tab = 2 })
                1 -> ManageScreen()
                else -> SettingsScreen(onSaved = { tab = 0 })
            }
        }
    }
}

@Composable
private fun FloatingNavItem(
    icon: ImageVector,
    label: String,
    selected: Boolean,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val tint = if (selected) {
        MaterialTheme.colorScheme.primary
    } else {
        MaterialTheme.colorScheme.onSurfaceVariant
    }
    Column(
        modifier = modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) sageFill() else MaterialTheme.colorScheme.surfaceVariant)
            .clickable(onClick = onClick)
            .padding(vertical = 7.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Icon(icon, contentDescription = label, tint = tint, modifier = Modifier.size(21.dp))
        Spacer(modifier = Modifier.height(1.dp))
        Text(text = label, style = MaterialTheme.typography.labelMedium, color = tint)
    }
}
