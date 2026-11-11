package com.wang4386.cdtmonitor.widget

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.io.IOException

/** Connection form: site URL + widget:read API key (Keystore-encrypted). */
@Composable
fun SettingsScreen(onSaved: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var siteUrl by remember { mutableStateOf(AppPreferences.siteUrl(context)) }
    var apiKeyInput by remember { mutableStateOf("") }
    var showKey by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var hasSaved by remember { mutableStateOf(AppPreferences.hasConnection(context)) }

    fun saveAndTest() {
        val url = siteUrl.trim()
        if (CdtApi.normalizeBaseUrl(url).isEmpty()) {
            message = "请输入有效的 http:// 或 https:// 站点地址"
            return
        }
        val effectiveKey = apiKeyInput.trim().ifEmpty { AppPreferences.apiKey(context) }
        if (effectiveKey.isEmpty()) {
            message = "请输入 API Key"
            return
        }
        busy = true
        message = "正在测试连接…"
        scope.launch {
            val result = withContext(Dispatchers.IO) {
                runCatching {
                    val summary = CdtApi.fetchSummary(url, effectiveKey)
                    if (!AppPreferences.saveConnection(
                            context,
                            CdtApi.normalizeBaseUrl(url),
                            effectiveKey,
                        )
                    ) {
                        throw IOException("Android Keystore 无法保存 API Key，请检查设备安全设置")
                    }
                    AppPreferences.saveLastSummary(context, summary)
                    summary
                }
            }
            busy = false
            result.onSuccess { summary ->
                hasSaved = true
                apiKeyInput = ""
                RefreshWorker.enqueuePeriodic(context)
                RefreshWorker.enqueueNow(context)
                message = "连接成功，发现 ${summary.accounts.size} 个实例。"
                onSaved()
            }.onFailure { error ->
                message = error.message ?: "连接失败，请检查站点地址和 API Key"
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 14.dp),
    ) {
        Text(
            text = "CDT MONITOR",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.primary,
        )
        Spacer(modifier = Modifier.height(3.dp))
        Text(
            text = "连接设置",
            style = MaterialTheme.typography.headlineMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
        Spacer(modifier = Modifier.height(8.dp))
        Text(
            text = "为桌面小组件和实例看板连接一个只读的 CDT Monitor API Key。",
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.height(16.dp))
        Card(
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(24.dp),
            colors = CardDefaults.cardColors(
                containerColor = MaterialTheme.colorScheme.surfaceVariant,
            ),
            elevation = CardDefaults.cardElevation(defaultElevation = 0.dp),
            border = androidx.compose.foundation.BorderStroke(
                1.dp,
                MaterialTheme.colorScheme.outlineVariant,
            ),
        ) {
            Column(modifier = Modifier.padding(20.dp)) {
                Text(
                    text = "站点地址",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.height(8.dp))
                OutlinedTextField(
                    value = siteUrl,
                    onValueChange = { siteUrl = it },
                    modifier = Modifier.fillMaxWidth(),
                    placeholder = { Text("例如 https://monitor.example.com") },
                    singleLine = true,
                    enabled = !busy,
                    shape = RoundedCornerShape(10.dp),
                )
                Spacer(modifier = Modifier.height(20.dp))
                Text(
                    text = "API Key",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.height(8.dp))
                OutlinedTextField(
                    value = apiKeyInput,
                    onValueChange = { apiKeyInput = it },
                    modifier = Modifier.fillMaxWidth(),
                    placeholder = { Text(if (hasSaved) "已保存，留空表示不修改" else "cdt_xxx") },
                    singleLine = true,
                    enabled = !busy,
                    shape = RoundedCornerShape(10.dp),
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
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = "请在 CDT Monitor 设置中创建只包含 widget:read scope 的 Key。Key 会使用本机 Android Keystore 加密保存。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
        Spacer(modifier = Modifier.height(18.dp))
        Button(
            onClick = { saveAndTest() },
            modifier = Modifier
                .fillMaxWidth()
                .height(52.dp),
            enabled = !busy,
            shape = RoundedCornerShape(999.dp),
        ) {
            Text(if (busy) "正在测试…" else "保存并测试连接")
        }
        if (message != null) {
            Spacer(modifier = Modifier.height(14.dp))
            Text(
                text = message!!,
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Spacer(modifier = Modifier.height(18.dp))
        Text(
            text = "小组件每 30 分钟自动刷新，也可点小组件上的刷新图标立即更新；把小组件拉大到 4×3 以上会显示全部实例列表。",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
