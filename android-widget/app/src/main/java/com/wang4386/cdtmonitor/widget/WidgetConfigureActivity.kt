package com.wang4386.cdtmonitor.widget

import android.appwidget.AppWidgetManager
import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.glance.appwidget.state.updateAppWidgetState
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class WidgetConfigureActivity : ComponentActivity() {
    private var appWidgetId: Int = AppWidgetManager.INVALID_APPWIDGET_ID

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setResult(RESULT_CANCELED)
        appWidgetId = intent.getIntExtra(
            AppWidgetManager.EXTRA_APPWIDGET_ID,
            AppWidgetManager.INVALID_APPWIDGET_ID,
        )
        if (appWidgetId == AppWidgetManager.INVALID_APPWIDGET_ID) {
            finish()
            return
        }
        setContent {
            CdtTheme {
                Surface(modifier = Modifier.fillMaxSize()) {
                    ConfigureScreen(
                        appWidgetId = appWidgetId,
                        onDone = {
                            val result = Intent()
                            result.putExtra(AppWidgetManager.EXTRA_APPWIDGET_ID, appWidgetId)
                            setResult(RESULT_OK, result)
                            finish()
                        },
                        onOpenSettings = {
                            startActivity(Intent(this, SettingsActivity::class.java))
                        },
                    )
                }
            }
        }
    }
}

@Composable
private fun ConfigureScreen(
    appWidgetId: Int,
    onDone: () -> Unit,
    onOpenSettings: () -> Unit,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var accounts by remember { mutableStateOf<List<CdtApi.Account>>(emptyList()) }
    var selectedId by remember { mutableStateOf<Long?>(null) }
    var hint by remember { mutableStateOf("正在读取实例…") }
    var hasConnection by remember { mutableStateOf(AppPreferences.hasConnection(context)) }
    var loadToken by remember { mutableStateOf(0) }

    LaunchedEffect(loadToken) {
        hasConnection = AppPreferences.hasConnection(context)
        if (!hasConnection) {
            hint = "请先保存站点地址和 API Key，再返回这里选择实例。"
            return@LaunchedEffect
        }
        // Show the cached summary immediately, then refresh in the background.
        AppPreferences.lastSummary(context)?.let { cached ->
            accounts = cached.accounts
            if (selectedId == null) selectedId = cached.accounts.firstOrNull()?.id
            hint = "选择一个实例后添加小组件。"
        }
        val fresh = withContext(Dispatchers.IO) {
            runCatching {
                CdtApi.fetchSummary(AppPreferences.siteUrl(context), AppPreferences.apiKey(context))
            }
        }
        fresh.onSuccess { summary ->
            AppPreferences.saveLastSummary(context, summary)
            accounts = summary.accounts
            if (accounts.none { it.id == selectedId }) {
                selectedId = accounts.firstOrNull()?.id
            }
            hint = if (accounts.isEmpty()) "API 没有返回可用实例。" else "选择一个实例后添加小组件。"
        }.onFailure { error ->
            if (accounts.isEmpty()) {
                hint = error.message ?: "读取实例失败，请检查连接设置"
            }
        }
    }

    fun confirm() {
        val accountId = selectedId ?: return
        scope.launch {
            AppPreferences.saveWidgetAccount(context, appWidgetId, accountId)
            // Push the selection (and any cached data) into the widget's Glance state.
            runCatching {
                val manager = GlanceAppWidgetManager(context)
                val glanceId = manager.getGlanceIds(CdtWidget::class.java)
                    .firstOrNull { manager.getAppWidgetId(it) == appWidgetId }
                if (glanceId != null) {
                    updateAppWidgetState(context, glanceId) { prefs ->
                        prefs[WidgetState.ACCOUNT_ID] = accountId
                        AppPreferences.lastSummary(context)?.let { cached ->
                            prefs[WidgetState.SUMMARY_JSON] = cached.toJson().toString()
                        }
                    }
                    CdtWidget().update(context, glanceId)
                }
            }
            RefreshWorker.enqueuePeriodic(context)
            RefreshWorker.enqueueNow(context, appWidgetId)
            onDone()
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .statusBarsPadding()
            .navigationBarsPadding()
            .padding(horizontal = 20.dp, vertical = 24.dp),
    ) {
        Text(
            text = "选择 CDT 实例",
            style = MaterialTheme.typography.titleLarge,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(modifier = Modifier.height(6.dp))
        Text(
            text = hint,
            fontSize = 13.sp,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.height(16.dp))
        if (!hasConnection) {
            Button(
                onClick = onOpenSettings,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
            ) {
                Text("去设置连接")
            }
            Spacer(modifier = Modifier.height(10.dp))
            OutlinedButton(
                onClick = { loadToken++ },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
            ) {
                Text("已保存，重新读取")
            }
        } else {
            LazyColumn(modifier = Modifier.weight(1f)) {
                items(accounts, key = { it.id }) { account ->
                    AccountCard(
                        account = account,
                        selected = account.id == selectedId,
                        onClick = { selectedId = account.id },
                    )
                    Spacer(modifier = Modifier.height(10.dp))
                }
            }
            Spacer(modifier = Modifier.height(10.dp))
            Button(
                onClick = { confirm() },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
                enabled = selectedId != null,
            ) {
                Text("添加小组件")
            }
        }
    }
}

@Composable
private fun AccountCard(
    account: CdtApi.Account,
    selected: Boolean,
    onClick: () -> Unit,
) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick),
        colors = CardDefaults.cardColors(
            containerColor = if (selected) {
                MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)
            } else {
                MaterialTheme.colorScheme.surfaceVariant
            },
        ),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            RadioButton(selected = selected, onClick = onClick)
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = account.displayName(),
                    fontSize = 15.sp,
                    fontWeight = FontWeight.Bold,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.height(2.dp))
                Text(
                    text = "${account.displayStatus()} · ${account.displayUsage()}",
                    fontSize = 12.sp,
                    color = if (account.isRunning()) CdtGreen else MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
