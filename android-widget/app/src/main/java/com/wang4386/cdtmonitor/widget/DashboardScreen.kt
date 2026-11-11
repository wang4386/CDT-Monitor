package com.wang4386.cdtmonitor.widget

import androidx.compose.animation.animateContentSize
import androidx.compose.animation.core.FastOutSlowInEasing
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.gestures.detectDragGestures
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountBalanceWallet
import androidx.compose.material.icons.filled.DataUsage
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.KeyboardArrowDown
import androidx.compose.material.icons.filled.Language
import androidx.compose.material.icons.filled.Paid
import androidx.compose.material.icons.filled.PowerSettingsNew
import androidx.compose.material.icons.filled.Public
import androidx.compose.material.icons.filled.Schedule
import androidx.compose.material.icons.filled.ShowChart
import androidx.compose.material.icons.filled.Speed
import androidx.compose.material.icons.filled.Update
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
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
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.Locale
import kotlin.math.roundToInt

/**
 * Dashboard styled after the user's reference (a Clash-style client):
 * pale sage ground, white cards with very large radii, a ring gauge for
 * fleet traffic, a smooth gradient area chart for the last 7 days, and
 * compact stat tiles. Motion follows the same soft feel — count-up
 * figures, a sweeping ring and animated bars (tweened, cheap to draw).
 *
 * Data: /api/v1/status + /api/v1/accounts/{id}/history (widget:read).
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DashboardScreen(onOpenSettings: () -> Unit) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    var status by remember { mutableStateOf(AppPreferences.lastStatus(context)) }
    var histories by remember { mutableStateOf<Map<Long, List<CdtApi.HistoryPoint>>>(emptyMap()) }
    var refreshing by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var hasConnection by remember { mutableStateOf(AppPreferences.hasConnection(context)) }
    var expandedId by remember { mutableStateOf<Long?>(null) }

    fun load() {
        if (!AppPreferences.hasConnection(context)) {
            hasConnection = false
            return
        }
        hasConnection = true
        refreshing = true
        scope.launch {
            val result = withContext(Dispatchers.IO) {
                runCatching {
                    val (fresh, raw) = CdtApi.fetchStatusWithRaw(
                        AppPreferences.siteUrl(context),
                        AppPreferences.apiKey(context),
                    )
                    AppPreferences.saveLastStatus(context, raw)
                    val loadedHistories = fresh.accounts.associate { account ->
                        account.id to runCatching {
                            CdtApi.fetchDailyHistory(
                                AppPreferences.siteUrl(context),
                                AppPreferences.apiKey(context),
                                account.id,
                            )
                        }.getOrDefault(emptyList())
                    }
                    fresh to loadedHistories
                }
            }
            refreshing = false
            result.onSuccess { (fresh, loadedHistories) ->
                status = fresh
                histories = loadedHistories
                error = null
                if (expandedId == null) {
                    expandedId = fresh.accounts.firstOrNull()?.id
                }
            }.onFailure { e ->
                error = e.message ?: "刷新失败"
            }
        }
    }

    LaunchedEffect(Unit) { load() }

    if (!hasConnection) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 24.dp),
            verticalArrangement = Arrangement.Center,
        ) {
            Text(
                text = "总览",
                style = MaterialTheme.typography.headlineMedium,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(modifier = Modifier.height(8.dp))
            Text(
                text = "还没有连接。填入站点地址和一个只读 API Key 后，这里会显示每个实例的流量、IP 和账单信息。",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.height(20.dp))
            Button(
                onClick = onOpenSettings,
                shape = RoundedCornerShape(999.dp),
            ) {
                Text("去填写连接信息")
            }
        }
        return
    }

    val accounts = status?.accounts.orEmpty()
    val chartAccount = accounts.firstOrNull { it.id == expandedId } ?: accounts.firstOrNull()

    PullToRefreshBox(
        isRefreshing = refreshing,
        onRefresh = { load() },
        modifier = Modifier.fillMaxSize(),
    ) {
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            contentPadding = PaddingValues(horizontal = 14.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            item {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = "总览",
                            style = MaterialTheme.typography.headlineMedium,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                        Text(
                            text = "CDT Monitor · ${accounts.size} 个实例",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    ConnectionPill(refreshing = refreshing, hasError = error != null) { load() }
                }
            }
            if (error != null) {
                item {
                    Text(
                        text = "刷新失败：$error（显示上次数据）",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }
            if (accounts.isNotEmpty()) {
                item { RingHeroCard(accounts, status?.systemLastRun.orEmpty()) }
                if (chartAccount != null) {
                    item {
                        ChartCard(
                            account = chartAccount,
                            daily = histories[chartAccount.id].orEmpty(),
                        )
                    }
                }
                item { LedgerTiles(accounts) }
                item {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        modifier = Modifier.padding(start = 2.dp, top = 2.dp),
                    ) {
                        CardIcon(Icons.Filled.Dns, boxSize = 24.dp, iconSize = 14.dp)
                        Spacer(modifier = Modifier.width(7.dp))
                        Text(
                            text = "实例",
                            style = MaterialTheme.typography.titleMedium,
                            color = MaterialTheme.colorScheme.onSurface,
                        )
                    }
                }
                items(accounts, key = { it.id }) { account ->
                    InstanceRow(
                        account = account,
                        expanded = expandedId == account.id,
                        onToggle = {
                            expandedId = if (expandedId == account.id) null else account.id
                        },
                        modifier = Modifier.animateItem(),
                    )
                }
                item {
                    Text(
                        text = "点按实例行展开明细 · 下拉可刷新",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(start = 4.dp),
                    )
                }
            } else if (!refreshing) {
                item {
                    Text(
                        text = "暂无实例数据，下拉刷新试试。",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

/** Outlined status pill in the header, like the reference's 已开启 toggle. */
@Composable
private fun ConnectionPill(refreshing: Boolean, hasError: Boolean, onClick: () -> Unit) {
    val tint = if (hasError) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.primary
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .border(1.dp, tint.copy(alpha = 0.55f), RoundedCornerShape(999.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(7.dp)
                .clip(CircleShape)
                .background(tint),
        )
        Spacer(modifier = Modifier.width(7.dp))
        Text(
            text = when {
                refreshing -> "更新中"
                hasError -> "刷新失败"
                else -> "已连接"
            },
            style = MaterialTheme.typography.labelLarge,
            color = tint,
        )
    }
}

/** Small sage rounded-square icon badge used on card headers and rows. */
@Composable
private fun CardIcon(
    icon: ImageVector,
    boxSize: Dp = 28.dp,
    iconSize: Dp = 16.dp,
) {
    Box(
        modifier = Modifier
            .size(boxSize)
            .clip(RoundedCornerShape(8.dp))
            .background(sageFill()),
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.primary,
            modifier = Modifier.size(iconSize),
        )
    }
}

/** White hero card: animated ring gauge + fleet traffic legend. */
@Composable
private fun RingHeroCard(accounts: List<CdtApi.StatusAccount>, lastRun: String) {
    val used = accounts.sumOf { it.flowUsed }
    val total = accounts.sumOf { it.flowTotal }
    val pct = if (total > 0) used / total * 100.0 else 0.0
    val running = accounts.count { it.isRunning() }

    val animatedUsed by animateFloatAsState(
        targetValue = used.toFloat(),
        animationSpec = tween(durationMillis = 900, easing = FastOutSlowInEasing),
        label = "usedCountUp",
    )
    val animatedPct by animateFloatAsState(
        targetValue = pct.toFloat(),
        animationSpec = tween(durationMillis = 900, easing = FastOutSlowInEasing),
        label = "ringSweep",
    )

    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(24.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CardIcon(Icons.Filled.DataUsage)
                Spacer(modifier = Modifier.width(8.dp))
                Text(
                    text = "流量用量",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.weight(1f))
                Text(
                    text = if (lastRun.isNotEmpty()) "更新 ${formatTimestamp(lastRun)}" else "全部实例",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(modifier = Modifier.height(12.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                RingGauge(pct = animatedPct.toDouble())
                Spacer(modifier = Modifier.width(16.dp))
                Column(modifier = Modifier.weight(1f)) {
                    LegendRow(
                        dot = MaterialTheme.colorScheme.primary,
                        label = "已用",
                        value = String.format(Locale.getDefault(), "%.2f GB", animatedUsed),
                        strong = true,
                    )
                    Spacer(modifier = Modifier.height(8.dp))
                    LegendRow(
                        dot = sageMid(),
                        label = "剩余",
                        value = String.format(
                            Locale.getDefault(),
                            "%.2f GB",
                            (total - used).coerceAtLeast(0.0),
                        ),
                        strong = false,
                    )
                    Spacer(modifier = Modifier.height(10.dp))
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Spacer(modifier = Modifier.height(8.dp))
                    Text(
                        text = "总配额 ${String.format(Locale.getDefault(), "%.0f GB", total)} · $running/${accounts.size} 运行中",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun LegendRow(dot: Color, label: String, value: String, strong: Boolean) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Box(
            modifier = Modifier
                .width(16.dp)
                .height(8.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(dot),
        )
        Spacer(modifier = Modifier.width(8.dp))
        Text(
            text = label,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(modifier = Modifier.weight(1f))
        Text(
            text = value,
            style = if (strong) {
                MaterialTheme.typography.titleMedium.copy(fontFeatureSettings = "tnum")
            } else {
                MaterialTheme.typography.bodyLarge.copy(fontFeatureSettings = "tnum")
            },
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** Donut gauge drawn on Canvas: sage track, deep-green sweep, % centered. */
@Composable
private fun RingGauge(pct: Double) {
    val track = sageFill()
    val sweep = MaterialTheme.colorScheme.primary
    val clamped = pct.coerceIn(0.0, 100.0)
    Box(
        modifier = Modifier.size(94.dp),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(modifier = Modifier.fillMaxSize()) {
            val strokeWidth = 9.dp.toPx()
            val inset = strokeWidth / 2f
            val arcSize = androidx.compose.ui.geometry.Size(
                size.width - strokeWidth,
                size.height - strokeWidth,
            )
            val topLeft = Offset(inset, inset)
            drawArc(
                color = track,
                startAngle = -90f,
                sweepAngle = 360f,
                useCenter = false,
                topLeft = topLeft,
                size = arcSize,
                style = Stroke(width = strokeWidth, cap = StrokeCap.Round),
            )
            if (clamped > 0.2) {
                drawArc(
                    color = sweep,
                    startAngle = -90f,
                    sweepAngle = (360.0 * clamped / 100.0).toFloat(),
                    useCenter = false,
                    topLeft = topLeft,
                    size = arcSize,
                    style = Stroke(width = strokeWidth, cap = StrokeCap.Round),
                )
            }
        }
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Text(
                text = String.format(Locale.getDefault(), "%.1f%%", pct),
                style = MaterialTheme.typography.titleLarge.copy(fontFeatureSettings = "tnum"),
                color = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = "已用",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/**
 * Smooth gradient area chart of the last 7 daily points. The history API
 * returns cumulative used-traffic snapshots, so the curve is the used
 * level over time; the header reports growth across the period.
 */
@Composable
private fun ChartCard(account: CdtApi.StatusAccount, daily: List<CdtApi.HistoryPoint>) {
    Card(
        modifier = Modifier.fillMaxWidth(),
        shape = RoundedCornerShape(24.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            val last7 = daily.takeLast(7)
            val growth = if (last7.size >= 2) last7.last().traffic - last7.first().traffic else 0.0
            Row(verticalAlignment = Alignment.CenterVertically) {
                CardIcon(Icons.Filled.ShowChart)
                Spacer(modifier = Modifier.width(8.dp))
                Text(
                    text = "近 7 日已用流量",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(modifier = Modifier.weight(1f))
                Text(
                    text = "增长 +${String.format(Locale.getDefault(), "%.2f GB", growth)}",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.primary,
                )
            }
            Text(
                text = account.displayName(),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(modifier = Modifier.height(8.dp))
            if (last7.size >= 2) {
                AreaChart(last7)
            } else {
                Text(
                    text = "历史数据不足，等待更多采集点。",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun AreaChart(points: List<CdtApi.HistoryPoint>) {
    val lineColor = MaterialTheme.colorScheme.primary
    val labelColor = MaterialTheme.colorScheme.onSurfaceVariant
    val paperColor = MaterialTheme.colorScheme.surfaceVariant
    var selectedIndex by remember(points) { mutableStateOf<Int?>(null) }
    fun pickIndex(x: Float, width: Float): Int {
        if (points.size < 2 || width <= 0f) return 0
        val fraction = (x / width).coerceIn(0f, 1f)
        return (fraction * (points.size - 1)).roundToInt().coerceIn(0, points.lastIndex)
    }
    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(104.dp)
            .pointerInput(points) {
                detectTapGestures { offset ->
                    selectedIndex = pickIndex(offset.x, size.width.toFloat())
                }
            }
            .pointerInput(points) {
                detectDragGestures(
                    onDragStart = { offset ->
                        selectedIndex = pickIndex(offset.x, size.width.toFloat())
                    },
                    onDrag = { change, _ ->
                        selectedIndex = pickIndex(change.position.x, size.width.toFloat())
                    },
                )
            },
    ) {
        val labelBand = 16.dp.toPx()
        val chartHeight = size.height - labelBand
        val values = points.map { it.traffic }
        // Honest scale: baseline 0, so slopes mean what they look like.
        val maxV = (values.max() * 1.08).coerceAtLeast(1.0)
        val stepX = size.width / (points.size - 1).coerceAtLeast(1)
        val topPad = 14.dp.toPx()
        val coords = values.mapIndexed { i, v ->
            val y = topPad + (1f - (v / maxV).toFloat()) * (chartHeight - topPad - 4.dp.toPx())
            Offset(stepX * i, y)
        }
        // Polyline through the exact data points (no smoothing drift).
        val line = Path()
        line.moveTo(coords.first().x, coords.first().y)
        for (i in 1 until coords.size) {
            line.lineTo(coords[i].x, coords[i].y)
        }
        val fill = Path()
        fill.addPath(line)
        fill.lineTo(size.width, chartHeight)
        fill.lineTo(0f, chartHeight)
        fill.close()
        drawPath(
            path = fill,
            brush = Brush.verticalGradient(
                colors = listOf(lineColor.copy(alpha = 0.26f), lineColor.copy(alpha = 0f)),
                startY = 0f,
                endY = chartHeight,
            ),
        )
        drawPath(path = line, color = lineColor, style = Stroke(width = 2.dp.toPx()))
        coords.forEachIndexed { index, point ->
            val isLast = index == coords.lastIndex
            drawCircle(
                color = lineColor,
                radius = if (isLast) 4.dp.toPx() else 3.dp.toPx(),
                center = point,
            )
            if (!isLast) {
                drawCircle(color = Color.White, radius = 1.4.dp.toPx(), center = point)
            }
        }
        // Selected point: guide line, halo dot and a value pill.
        val sel = selectedIndex
        if (sel != null && sel in coords.indices) {
            val point = coords[sel]
            drawLine(
                color = labelColor.copy(alpha = 0.45f),
                start = Offset(point.x, topPad - 6.dp.toPx()),
                end = Offset(point.x, chartHeight),
                strokeWidth = 1.dp.toPx(),
            )
            drawCircle(color = lineColor, radius = 6.dp.toPx(), center = point)
            drawCircle(color = paperColor, radius = 2.8.dp.toPx(), center = point)
        }
        // Labels drawn in-canvas so they sit exactly under their points.
        fun Color.toArgbInt(): Int = android.graphics.Color.argb(
            (alpha * 255).toInt(), (red * 255).toInt(), (green * 255).toInt(), (blue * 255).toInt(),
        )
        val paint = android.graphics.Paint().apply {
            isAntiAlias = true
            textSize = 10.dp.toPx()
            color = labelColor.toArgbInt()
        }
        coords.forEachIndexed { index, point ->
            paint.textAlign = when (index) {
                0 -> android.graphics.Paint.Align.LEFT
                coords.lastIndex -> android.graphics.Paint.Align.RIGHT
                else -> android.graphics.Paint.Align.CENTER
            }
            drawContext.canvas.nativeCanvas.drawText(
                points[index].at.substring(5, 10),
                point.x,
                size.height - 2.dp.toPx(),
                paint,
            )
        }
        if (sel != null && sel in coords.indices) {
            // Value pill for the selected point.
            val point = coords[sel]
            val valueText = String.format(Locale.getDefault(), "%.2f GB", values[sel])
            val dateText = points[sel].at.substring(5, 10)
            val valuePaint = android.graphics.Paint().apply {
                isAntiAlias = true
                textSize = 11.5.dp.toPx()
                color = lineColor.toArgbInt()
                typeface = android.graphics.Typeface.create(
                    android.graphics.Typeface.DEFAULT,
                    android.graphics.Typeface.BOLD,
                )
            }
            val datePaint = android.graphics.Paint().apply {
                isAntiAlias = true
                textSize = 9.5.dp.toPx()
                color = labelColor.toArgbInt()
            }
            val pillWidth = maxOf(
                valuePaint.measureText(valueText),
                datePaint.measureText(dateText),
            ) + 16.dp.toPx()
            val pillHeight = 30.dp.toPx()
            var pillLeft = point.x - pillWidth / 2
            pillLeft = pillLeft.coerceIn(0f, size.width - pillWidth)
            var pillTop = point.y - pillHeight - 10.dp.toPx()
            if (pillTop < 0f) pillTop = point.y + 10.dp.toPx()
            val bgPaint = android.graphics.Paint().apply {
                isAntiAlias = true
                color = paperColor.toArgbInt()
                style = android.graphics.Paint.Style.FILL
            }
            val borderPaint = android.graphics.Paint().apply {
                isAntiAlias = true
                color = lineColor.copy(alpha = 0.35f).toArgbInt()
                style = android.graphics.Paint.Style.STROKE
                strokeWidth = 1.dp.toPx()
            }
            val rect = android.graphics.RectF(pillLeft, pillTop, pillLeft + pillWidth, pillTop + pillHeight)
            drawContext.canvas.nativeCanvas.drawRoundRect(rect, 8.dp.toPx(), 8.dp.toPx(), bgPaint)
            drawContext.canvas.nativeCanvas.drawRoundRect(rect, 8.dp.toPx(), 8.dp.toPx(), borderPaint)
            valuePaint.textAlign = android.graphics.Paint.Align.CENTER
            datePaint.textAlign = android.graphics.Paint.Align.CENTER
            drawContext.canvas.nativeCanvas.drawText(
                valueText, pillLeft + pillWidth / 2, pillTop + 13.5.dp.toPx(), valuePaint,
            )
            drawContext.canvas.nativeCanvas.drawText(
                dateText, pillLeft + pillWidth / 2, pillTop + 25.dp.toPx(), datePaint,
            )
        }
        // First / last values above their points (hidden while scrubbing).
        if (selectedIndex == null) {
        paint.textSize = 11.dp.toPx()
        paint.color = lineColor.toArgbInt()
        paint.textAlign = android.graphics.Paint.Align.LEFT
        drawContext.canvas.nativeCanvas.drawText(
            String.format(Locale.getDefault(), "%.1f", values.first()),
            coords.first().x,
            coords.first().y - 8.dp.toPx(),
            paint,
        )
        paint.textAlign = android.graphics.Paint.Align.RIGHT
        drawContext.canvas.nativeCanvas.drawText(
            String.format(Locale.getDefault(), "%.1f GB", values.last()),
            coords.last().x,
            coords.last().y - 8.dp.toPx(),
            paint,
        )
        }
    }
}

/** Two stat tiles: monthly cost and account balance. */
@Composable
private fun LedgerTiles(accounts: List<CdtApi.StatusAccount>) {
    val costs = accounts.mapNotNull { it.monthlyCost }
    val balances = accounts.mapNotNull { it.balance }
    val sample = accounts.firstOrNull()
    Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
        StatTile(
            icon = Icons.Filled.Paid,
            label = "本月费用",
            value = if (costs.isEmpty()) "—" else sample?.displayMoney(costs.sum()) ?: "—",
            caption = if (costs.size > 1) "${costs.size} 个实例合计" else "当前计费周期",
            modifier = Modifier.weight(1f),
        )
        StatTile(
            icon = Icons.Filled.AccountBalanceWallet,
            label = "账户余额",
            value = if (balances.isEmpty()) "—" else sample?.displayMoney(balances.max()) ?: "—",
            caption = "阿里云账户",
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun StatTile(icon: ImageVector, label: String, value: String, caption: String, modifier: Modifier = Modifier) {
    Card(
        modifier = modifier,
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(modifier = Modifier.padding(14.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                CardIcon(icon, boxSize = 24.dp, iconSize = 14.dp)
                Spacer(modifier = Modifier.width(7.dp))
                Text(
                    text = label,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            Spacer(modifier = Modifier.height(7.dp))
            Text(
                text = value,
                style = MaterialTheme.typography.titleLarge.copy(
                    fontFamily = FontFamily.Monospace,
                    fontFeatureSettings = "tnum",
                ),
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(modifier = Modifier.height(3.dp))
            Text(
                text = caption,
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

/** Instance card: sage chip status, animated thick bar, expandable detail. */
@Composable
private fun InstanceRow(
    account: CdtApi.StatusAccount,
    expanded: Boolean,
    onToggle: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Card(
        modifier = modifier.fillMaxWidth(),
        shape = RoundedCornerShape(20.dp),
        colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant),
        elevation = CardDefaults.cardElevation(defaultElevation = 2.dp),
    ) {
        Column(
            modifier = Modifier
                .padding(14.dp)
                .animateContentSize(),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(onClick = onToggle),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                CardIcon(Icons.Filled.Dns, boxSize = 36.dp, iconSize = 19.dp)
                Spacer(modifier = Modifier.width(10.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = account.displayName(),
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.onSurface,
                        maxLines = 1,
                    )
                    val sub = account.displayRegion()
                    if (sub.isNotEmpty()) {
                        Text(
                            text = sub,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                        )
                    }
                }
                StatusChip(account)
                Spacer(modifier = Modifier.width(4.dp))
                val chevronRotation by animateFloatAsState(
                    targetValue = if (expanded) 180f else 0f,
                    animationSpec = tween(220),
                    label = "chevron",
                )
                Icon(
                    Icons.Filled.KeyboardArrowDown,
                    contentDescription = if (expanded) "收起明细" else "展开明细",
                    tint = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier
                        .size(20.dp)
                        .rotate(chevronRotation),
                )
            }
            Spacer(modifier = Modifier.height(10.dp))
            AnimatedBar(account.percentage, account.overThreshold)
            Spacer(modifier = Modifier.height(7.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = String.format(
                        Locale.getDefault(),
                        "已用 %.2f GB · 剩余 %.2f GB",
                        account.flowUsed,
                        (account.flowTotal - account.flowUsed).coerceAtLeast(0.0),
                    ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = String.format(Locale.getDefault(), "%.1f%%", account.percentage),
                    style = MaterialTheme.typography.titleSmall.copy(fontFeatureSettings = "tnum"),
                    color = if (account.overThreshold) MaterialTheme.colorScheme.error
                    else MaterialTheme.colorScheme.onSurface,
                )
            }
            if (expanded) {
                Spacer(modifier = Modifier.height(10.dp))
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                Spacer(modifier = Modifier.height(10.dp))
                SelectionContainer {
                    Column {
                if (account.publicIp.isNotEmpty()) {
                    DetailRow(Icons.Filled.Public, "公网 IPv4", account.publicIp)
                }
                if (account.publicIpv6.isNotEmpty()) {
                    DetailRow(Icons.Filled.Language, "公网 IPv6", account.publicIpv6, small = true)
                }
                DetailRow(Icons.Filled.Paid, "本月费用", account.displayMoney(account.monthlyCost))
                DetailRow(Icons.Filled.AccountBalanceWallet, "账户余额", account.displayMoney(account.balance))
                if (account.threshold > 0) {
                    DetailRow(Icons.Filled.Speed, "流量阈值", "${account.threshold}%")
                }
                if (account.scheduleEnabled) {
                    DetailRow(Icons.Filled.Schedule, "定时开关机", "${account.startTime} 开 · ${account.stopTime} 关")
                }
                if (account.keepAlive != null) {
                    DetailRow(Icons.Filled.PowerSettingsNew, "保活", if (account.keepAlive == true) "开启" else "关闭")
                }
                DetailRow(Icons.Filled.Update, "更新时间", formatTimestamp(account.lastUpdated))
                if (account.stale) {
                    Spacer(modifier = Modifier.height(6.dp))
                    Text(
                        text = "数据已过期，等待下一轮采集",
                        style = MaterialTheme.typography.labelSmall,
                        color = CdtOrange,
                    )
                }
                if (account.billingError.isNotEmpty()) {
                    Spacer(modifier = Modifier.height(6.dp))
                    Text(
                        text = "账单获取失败：${account.billingError}",
                        style = MaterialTheme.typography.labelSmall,
                        color = CdtOrange,
                    )
                }
                    }
                }
            }
        }
    }
}

/** Small sage chip with a status dot, echoing the reference's mode chips. */
@Composable
private fun StatusChip(account: CdtApi.StatusAccount) {
    val running = account.isRunning()
    val tint = if (running) MaterialTheme.colorScheme.primary else CdtOrange
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(sageFill())
            .padding(horizontal = 10.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            modifier = Modifier
                .size(6.dp)
                .clip(CircleShape)
                .background(tint),
        )
        Spacer(modifier = Modifier.width(5.dp))
        Text(
            text = account.displayStatus(),
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurface,
        )
    }
}

/** Animated 9dp usage bar on a sage track. */
@Composable
private fun AnimatedBar(percentage: Double, over: Boolean) {
    val animated by animateFloatAsState(
        targetValue = (percentage / 100.0).toFloat().coerceIn(0f, 1f),
        animationSpec = tween(durationMillis = 800, easing = FastOutSlowInEasing),
        label = "barFill",
    )
    val fill = when {
        over || percentage >= 90.0 -> MaterialTheme.colorScheme.error
        percentage >= 75.0 -> Color(0xFFE07B00)
        else -> MaterialTheme.colorScheme.primary
    }
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .height(8.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(sageFill()),
    ) {
        Box(
            modifier = Modifier
                .fillMaxWidth(animated)
                .height(8.dp)
                .clip(RoundedCornerShape(999.dp))
                .background(fill),
        )
    }
}

@Composable
private fun DetailRow(icon: ImageVector, label: String, value: String, small: Boolean = false) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        verticalAlignment = Alignment.Top,
    ) {
        Icon(
            icon,
            contentDescription = null,
            tint = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier
                .size(15.dp)
                // Sit on the label's first text line, not on the row's top
                // edge (the label's line box is taller than the glyph).
                .offset(y = 2.dp),
        )
        Spacer(modifier = Modifier.width(7.dp))
        Text(
            text = label,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = 2.dp, end = 16.dp),
        )
        Text(
            text = value,
            style = MaterialTheme.typography.bodyLarge.copy(
                fontFamily = FontFamily.Monospace,
                fontFeatureSettings = "tnum",
                fontSize = if (small) 11.5.sp else 13.5.sp,
                lineHeight = if (small) 15.sp else 18.sp,
            ),
            color = MaterialTheme.colorScheme.onSurface,
            textAlign = TextAlign.End,
            modifier = Modifier.weight(1f),
        )
    }
}
