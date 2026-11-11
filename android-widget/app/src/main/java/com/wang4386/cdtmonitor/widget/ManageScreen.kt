package com.wang4386.cdtmonitor.widget

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.OpenInNew
import androidx.compose.material.icons.filled.PowerSettingsNew
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

/**
 * Instance management tab.
 *
 * Power actions (start / stop / refresh) go through the API with a key
 * that carries the instance:control scope — the user creates it in the
 * Web console (API keys cannot be minted from inside the app). Schedule
 * editing stays on the Web: the backend's settings endpoint requires an
 * admin session, so schedules are shown read-only here with a jump-off
 * button. A Web-console shortcut is always visible at the top.
 */
@Composable
fun ManageScreen() {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var hasControlKey by remember { mutableStateOf(AppPreferences.hasControlKey(context)) }
    var status by remember { mutableStateOf(AppPreferences.lastStatus(context)) }
    var error by remember { mutableStateOf<String?>(null) }
    var loading by remember { mutableStateOf(false) }
    var jobTexts by remember { mutableStateOf<Map<Long, String>>(emptyMap()) }
    var pendingAction by remember { mutableStateOf<Pair<CdtApi.StatusAccount, String>?>(null) }

    fun openWeb() {
        val url = AppPreferences.siteUrl(context)
        if (url.isEmpty()) return
        runCatching {
            context.startActivity(
                Intent(Intent.ACTION_VIEW, Uri.parse(url))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
            )
        }
    }

    fun load() {
        if (!AppPreferences.hasControlKey(context)) return
        loading = true
        scope.launch {
            val result = withContext(Dispatchers.IO) {
                runCatching {
                    val (fresh, raw) = CdtApi.fetchStatusWithRaw(
                        AppPreferences.siteUrl(context),
                        AppPreferences.controlKey(context),
                    )
                    AppPreferences.saveLastStatus(context, raw)
                    fresh
                }
            }
            loading = false
            result.onSuccess { status = it; error = null }
                .onFailure { error = it.message ?: "加载失败" }
        }
    }

    fun runJob(account: CdtApi.StatusAccount, label: String, enqueue: () -> String) {
        jobTexts = jobTexts + (account.id to "$label 已提交…")
        scope.launch {
            val outcome = withContext(Dispatchers.IO) {
                runCatching {
                    val jobId = enqueue()
                    if (jobId.isEmpty()) return@runCatching "completed" to ""
                    var state = CdtApi.JobState("queued", "")
                    var polls = 0
                    while (!state.isDone && polls < 45) {
                        delay(2_000)
                        state = CdtApi.fetchJob(
                            AppPreferences.siteUrl(context),
                            AppPreferences.controlKey(context),
                            jobId,
                        )
                        polls++
                    }
                    state.status to state.error
                }
            }
            outcome.onSuccess { (jobStatus, jobError) ->
                jobTexts = jobTexts + (account.id to when (jobStatus) {
                    "completed" -> "$label 完成"
                    "failed" -> "$label 失败${if (jobError.isNotEmpty()) "：$jobError" else ""}"
                    else -> "$label 仍在执行，请稍后刷新查看"
                })
                load()
            }.onFailure { e ->
                jobTexts = jobTexts + (account.id to "$label 提交失败：${e.message ?: "未知错误"}")
            }
        }
    }

    LaunchedEffect(hasControlKey) { if (hasControlKey) load() }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 14.dp, vertical = 10.dp),
        verticalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        item {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = "管理",
                        style = MaterialTheme.typography.headlineMedium,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Text(
                        text = "实例控制 · 开关机与编排",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                OutlinedButton(
                    onClick = { openWeb() },
                    shape = RoundedCornerShape(999.dp),
                    enabled = AppPreferences.siteUrl(context).isNotEmpty(),
                ) {
                    Icon(Icons.Filled.OpenInNew, contentDescription = null)
                    Spacer(modifier = Modifier.width(6.dp))
                    Text("Web 控制台")
                }
            }
        }

        if (!AppPreferences.hasConnection(context) && !hasControlKey) {
            item {
                InfoCard(
                    title = "先完成连接设置",
                    body = "管理功能与看板共用站点地址。请先到「设置」页填入站点地址和只读 Key，再回来添加控制 Key。",
                )
            }
        } else if (!hasControlKey) {
            item {
                ControlKeyGuide(
                    onOpenWeb = { openWeb() },
                    onSaved = { hasControlKey = true },
                )
            }
        } else {
            if (error != null) {
                item {
                    Text(
                        text = "加载失败：$error",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
            val accounts = status?.accounts.orEmpty()
            items(accounts, key = { it.id }) { account ->
                ControlCard(
                    account = account,
                    jobText = jobTexts[account.id],
                    busy = jobTexts[account.id]?.endsWith("已提交…") == true,
                    onPower = { action -> pendingAction = account to action },
                    onRefresh = {
                        runJob(account, "刷新") {
                            CdtApi.refreshAccount(
                                AppPreferences.siteUrl(context),
                                AppPreferences.controlKey(context),
                                account.id,
                            )
                        }
                    },
                    onOpenWeb = { openWeb() },
                )
            }
            if (accounts.isEmpty() && !loading) {
                item {
                    InfoCard(title = "暂无实例", body = "控制 Key 已保存，但没有读到实例数据，下拉或稍后再试。")
                }
            }
            item {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    TextButton(onClick = { load() }) { Text("重新加载") }
                    Spacer(modifier = Modifier.weight(1f))
                    TextButton(
                        onClick = {
                            AppPreferences.clearControlKey(context)
                            hasControlKey = false
                            jobTexts = emptyMap()
                        },
                    ) {
                        Text("移除控制 Key", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }

    val pending = pendingAction
    if (pending != null) {
        val (account, action) = pending
        val isStart = action == "start"
        AlertDialog(
            onDismissRequest = { pendingAction = null },
            title = { Text(if (isStart) "确认开机" else "确认关机") },
            text = {
                Text(
                    if (isStart) {
                        "要启动实例「${account.displayName()}」吗？"
                    } else {
                        "要关闭实例「${account.displayName()}」吗？关机后流量监控与保活将暂停。"
                    },
                )
            },
            confirmButton = {
                Button(
                    onClick = {
                        pendingAction = null
                        runJob(account, if (isStart) "开机" else "关机") {
                            CdtApi.controlAction(
                                AppPreferences.siteUrl(context),
                                AppPreferences.controlKey(context),
                                account.id,
                                action,
                            )
                        }
                    },
                    colors = if (isStart) {
                        ButtonDefaults.buttonColors()
                    } else {
                        ButtonDefaults.buttonColors(
                            containerColor = MaterialTheme.colorScheme.error,
                        )
                    },
                ) {
                    Text(if (isStart) "开机" else "关机")
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingAction = null }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun InfoCard(title: String, body: String) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = title,
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = body,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** Guide + form shown until a control key is saved. */
@Composable
private fun ControlKeyGuide(onOpenWeb: () -> Unit, onSaved: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var key by remember { mutableStateOf("") }
    var showKey by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = "添加控制权限 Key",
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(modifier = Modifier.height(6.dp))
            Text(
                text = "开关机需要带控制权限的 API Key（应用内不能创建）：\n" +
                    "1. 打开 Web 控制台并登录\n" +
                    "2. 在「API Keys」里新建 Key，勾选 widget:read 与 instance:control\n" +
                    "3. 把生成的 Key 粘贴到下面保存",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.height(12.dp))
            OutlinedButton(
                onClick = onOpenWeb,
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(999.dp),
            ) {
                Icon(Icons.Filled.OpenInNew, contentDescription = null)
                Spacer(modifier = Modifier.width(6.dp))
                Text("打开 Web 控制台创建 Key")
            }
            Spacer(modifier = Modifier.height(12.dp))
            OutlinedTextField(
                value = key,
                onValueChange = { key = it },
                label = { Text("控制 Key") },
                singleLine = true,
                visualTransformation = if (showKey) {
                    VisualTransformation.None
                } else {
                    PasswordVisualTransformation()
                },
                trailingIcon = {
                    TextButton(onClick = { showKey = !showKey }) {
                        Text(if (showKey) "隐藏" else "显示")
                    }
                },
                modifier = Modifier.fillMaxWidth(),
                shape = RoundedCornerShape(12.dp),
            )
            Spacer(modifier = Modifier.height(12.dp))
            Button(
                onClick = {
                    busy = true
                    message = null
                    scope.launch {
                        val result = withContext(Dispatchers.IO) {
                            runCatching {
                                CdtApi.fetchStatus(
                                    AppPreferences.siteUrl(context),
                                    key.trim(),
                                )
                            }
                        }
                        busy = false
                        result.onSuccess {
                            if (AppPreferences.saveControlKey(context, key.trim())) {
                                message = "已保存，正在加载实例…"
                                onSaved()
                            } else {
                                message = "保存失败：本机加密存储不可用"
                            }
                        }.onFailure {
                            message = "验证失败：${it.message ?: "请检查 Key"}"
                        }
                    }
                },
                modifier = Modifier
                    .fillMaxWidth()
                    .height(48.dp),
                enabled = !busy && key.isNotBlank(),
                shape = RoundedCornerShape(999.dp),
            ) {
                Text(if (busy) "正在验证…" else "保存并验证")
            }
            if (message != null) {
                Spacer(modifier = Modifier.height(10.dp))
                Text(
                    text = message!!,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = "控制 Key 与只读 Key 分开加密保存在本机 Keystore 中。",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** One instance's control card: power, refresh, read-only schedule. */
@Composable
private fun ControlCard(
    account: CdtApi.StatusAccount,
    jobText: String?,
    busy: Boolean,
    onPower: (String) -> Unit,
    onRefresh: () -> Unit,
    onOpenWeb: () -> Unit,
) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CardIconBadge(Icons.Filled.Dns)
                Spacer(modifier = Modifier.width(10.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = account.displayName(),
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                    )
                    Text(
                        text = listOf(account.displayRegion(), account.publicIp)
                            .filter { it.isNotEmpty() }.joinToString(" · "),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                    )
                }
                Text(
                    text = account.displayStatus(),
                    style = MaterialTheme.typography.labelLarge,
                    color = if (account.isRunning()) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        CdtOrange
                    },
                )
            }
            Spacer(modifier = Modifier.height(10.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(
                    Icons.Filled.Schedule,
                    contentDescription = null,
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.height(16.dp),
                )
                Spacer(modifier = Modifier.width(6.dp))
                Text(
                    text = if (account.scheduleEnabled) {
                        "定时开关机 ${account.startTime} 开 · ${account.stopTime} 关"
                    } else {
                        "定时开关机未开启"
                    },
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onOpenWeb) { Text("去 Web 编排") }
            }
            Spacer(modifier = Modifier.height(6.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (account.isRunning()) {
                    Button(
                        onClick = { onPower("stop") },
                        enabled = !busy,
                        shape = RoundedCornerShape(999.dp),
                        colors = ButtonDefaults.buttonColors(
                            containerColor = MaterialTheme.colorScheme.error,
                        ),
                    ) {
                        Icon(Icons.Filled.PowerSettingsNew, contentDescription = null)
                        Spacer(modifier = Modifier.width(5.dp))
                        Text("关机")
                    }
                } else {
                    Button(
                        onClick = { onPower("start") },
                        enabled = !busy,
                        shape = RoundedCornerShape(999.dp),
                    ) {
                        Icon(Icons.Filled.PowerSettingsNew, contentDescription = null)
                        Spacer(modifier = Modifier.width(5.dp))
                        Text("开机")
                    }
                }
                OutlinedButton(
                    onClick = onRefresh,
                    enabled = !busy,
                    shape = RoundedCornerShape(999.dp),
                ) {
                    Icon(Icons.Filled.Refresh, contentDescription = null)
                    Spacer(modifier = Modifier.width(5.dp))
                    Text("刷新数据")
                }
            }
            if (jobText != null) {
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = jobText,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/** Sage icon badge shared by manage cards (mirrors the dashboard's). */
@Composable
private fun CardIconBadge(icon: androidx.compose.ui.graphics.vector.ImageVector) {
    androidx.compose.foundation.layout.Box(
        modifier = Modifier
            .size(36.dp)
            .clip(RoundedCornerShape(10.dp))
            .background(sageFill()),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(19.dp),
        )
    }
}
