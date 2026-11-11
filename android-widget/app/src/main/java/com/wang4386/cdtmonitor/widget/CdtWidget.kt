package com.wang4386.cdtmonitor.widget

import android.content.Context
import android.content.Intent
import android.content.res.Configuration
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.datastore.preferences.core.Preferences
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.GlanceTheme
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.LocalContext
import androidx.glance.LocalGlanceId
import androidx.glance.LocalSize
import androidx.glance.action.actionParametersOf
import androidx.glance.action.clickable
import androidx.glance.appwidget.AppWidgetId
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.SizeMode
import androidx.glance.appwidget.action.actionRunCallback
import androidx.glance.appwidget.action.actionStartActivity
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.provideContent
import androidx.glance.background
import androidx.glance.color.ColorProvider
import androidx.glance.currentState
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.ContentScale
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.state.PreferencesGlanceStateDefinition
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/**
 * CDT Monitor home-screen widget, rebuilt on Jetpack Glance.
 *
 * Layout patterns follow Google's canonical widget layouts (text + toolbar
 * rows, 16dp padding, status pill) from the official glance-canonical-layouts
 * skill; refresh/state handling follows glance-appwidget-updates-and-memory
 * (WorkManager fetches, DataStore state pushes, stale-data fallback).
 *
 * Three responsive buckets:
 * - SMALL: one instance, condensed.
 * - MEDIUM: one instance, full detail + refresh button.
 * - LARGE: all instances as a tappable list (tap pins that instance).
 */
class CdtWidget : GlanceAppWidget() {

    override val stateDefinition = PreferencesGlanceStateDefinition

    // Single mode: LocalSize is the widget's real size, and each layout
    // fills it exactly. (Responsive buckets capped the content height and
    // left large empty bands on taller placements.)
    override val sizeMode: SizeMode = SizeMode.Exact

    override suspend fun provideGlance(context: Context, id: GlanceId) {
        // First placement before any fetch: kick the worker so the widget
        // populates itself instead of waiting for the periodic run.
        if (AppPreferences.hasConnection(context) && AppPreferences.lastSummary(context) == null) {
            RefreshWorker.enqueueNow(context)
        }
        provideContent {
            GlanceTheme {
                WidgetContent()
            }
        }
    }
}

private val GreenProvider = ColorProvider(day = Color(0xFF1E5B4A), night = Color(0xFF3FA67F))
private val OrangeProvider = ColorProvider(day = Color(0xFFE07B00), night = Color(0xFFFFB340))
private val GreyProvider = ColorProvider(day = Color(0xFF6E7F76), night = Color(0xFF9DB0A5))
private val RedProvider = ColorProvider(day = Color(0xFFD93A2B), night = Color(0xFFFF6B5E))

// iOS system palette: grouped greys, near-black ink, one system green.
private val InkProvider = ColorProvider(day = Color(0xFF15211B), night = Color(0xFFEFF4F0))
private val MutedProvider = ColorProvider(day = Color(0xFF6E7F76), night = Color(0xFF9DB0A5))
private val TrackProvider = ColorProvider(day = Color(0xFFDCE9E0), night = Color(0xFF26332B))
private val PaperProvider = ColorProvider(day = Color(0xFFFFFFFF), night = Color(0xFFFFFFFF))

private fun statusColorProvider(status: String) = when {
    status.equals("Running", ignoreCase = true) -> GreenProvider
    status.equals("Stopped", ignoreCase = true) -> OrangeProvider
    else -> GreyProvider
}

@Composable
private fun WidgetContent() {
    val context = LocalContext.current
    val prefs = currentState<Preferences>()
    val size = LocalSize.current
    val glanceId = LocalGlanceId.current
    val appWidgetId = (glanceId as? AppWidgetId)?.appWidgetId ?: -1

    val selectedId = WidgetState.accountId(
        prefs,
        if (appWidgetId > 0) AppPreferences.widgetAccount(context, appWidgetId) else -1L,
    )
    val summary = WidgetState.summary(prefs) ?: AppPreferences.lastSummary(context)
    val history = WidgetState.history(prefs)
    val error = prefs[WidgetState.ERROR].orEmpty()
    val loading = prefs[WidgetState.LOADING] == true
    // Full-detail record for the selected instance (IP / billing), cached by
    // the dashboard and by RefreshWorker from /api/v1/status.
    val selectedAccount = summary?.let { it.find(selectedId) ?: it.accounts.firstOrNull() }
    val detail = selectedAccount?.let { sel ->
        AppPreferences.lastStatus(context)?.accounts?.firstOrNull { it.id == sel.id }
    }

    Box(
        modifier = GlanceModifier
            .fillMaxSize()
            .background(ImageProvider(R.drawable.widget_bg))
            .clickable(
                actionStartActivity(
                    Intent(context, SettingsActivity::class.java)
                        .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK),
                ),
            ),
    ) {
        when {
            !AppPreferences.hasConnection(context) -> UnconfiguredContent()
            summary == null && error.isNotEmpty() -> ErrorContent(error)
            summary == null -> LoadingContent()
            size.height < 150.dp -> FlatContent(summary, selectedId, detail)
            size.height < 235.dp -> RichContent(summary, selectedId, error, loading, detail)
            else -> LargeContent(summary, selectedId, error)
        }
    }
}

@Composable
private fun UnconfiguredContent() {
    Column(modifier = GlanceModifier.fillMaxSize().padding(16.dp)) {
        Text(
            text = "CDT Monitor",
            style = TextStyle(
                color = InkProvider,
                fontSize = 16.sp,
                fontWeight = FontWeight.Bold,
            ),
        )
        Spacer(modifier = GlanceModifier.height(6.dp))
        Text(
            text = "点按完成连接设置，查看实例流量",
            style = TextStyle(color = MutedProvider, fontSize = 12.sp),
        )
    }
}

@Composable
private fun LoadingContent() {
    Column(modifier = GlanceModifier.fillMaxSize().padding(16.dp)) {
        Text(
            text = "CDT Monitor",
            style = TextStyle(
                color = InkProvider,
                fontSize = 16.sp,
                fontWeight = FontWeight.Bold,
            ),
        )
        Spacer(modifier = GlanceModifier.height(6.dp))
        Text(
            text = "正在更新…",
            style = TextStyle(color = MutedProvider, fontSize = 12.sp),
        )
    }
}

@Composable
private fun ErrorContent(error: String) {
    Column(modifier = GlanceModifier.fillMaxSize().padding(16.dp)) {
        Text(
            text = "更新失败",
            style = TextStyle(
                color = RedProvider,
                fontSize = 16.sp,
                fontWeight = FontWeight.Bold,
            ),
        )
        Spacer(modifier = GlanceModifier.height(6.dp))
        Text(
            text = error,
            style = TextStyle(color = MutedProvider, fontSize = 12.sp),
            maxLines = 2,
        )
        Spacer(modifier = GlanceModifier.height(6.dp))
        Text(
            text = "点按打开设置检查连接",
            style = TextStyle(color = MutedProvider, fontSize = 11.sp),
        )
    }
}

/**
 * Ring gauge rendered to a bitmap (Glance has no canvas): sage track,
 * semantic sweep, percentage and label baked in. Regenerated only when the
 * percentage or the system theme changes.
 */
private fun ringBitmap(pct: Double, dark: Boolean): Bitmap {
    val size = 288
    val bitmap = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val stroke = 30f
    val rect = RectF(stroke / 2 + 8, stroke / 2 + 8, size - stroke / 2 - 8, size - stroke / 2 - 8)
    val trackPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = stroke
        strokeCap = Paint.Cap.ROUND
        color = if (dark) 0xFF26332B.toInt() else 0xFFDCE9E0.toInt()
    }
    canvas.drawArc(rect, 0f, 360f, false, trackPaint)
    val p = pct.coerceIn(0.0, 100.0)
    if (p > 0.5) {
        val sweepPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            style = Paint.Style.STROKE
            strokeWidth = stroke
            strokeCap = Paint.Cap.ROUND
            color = when {
                p >= 90.0 -> if (dark) 0xFFFF6B5E.toInt() else 0xFFD93A2B.toInt()
                p >= 75.0 -> if (dark) 0xFFFFB340.toInt() else 0xFFE07B00.toInt()
                else -> if (dark) 0xFF3FA67F.toInt() else 0xFF1E5B4A.toInt()
            }
        }
        canvas.drawArc(rect, -90f, (p * 3.6).toFloat(), false, sweepPaint)
    }
    val ink = if (dark) 0xFFEFF4F0.toInt() else 0xFF15211B.toInt()
    val muted = if (dark) 0xFF9DB0A5.toInt() else 0xFF6E7F76.toInt()
    val pctPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = ink
        textSize = 58f
        textAlign = Paint.Align.CENTER
        typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
    }
    canvas.drawText(
        String.format(Locale.getDefault(), "%.1f%%", pct),
        size / 2f,
        size / 2f + 6f,
        pctPaint,
    )
    val labelPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = muted
        textSize = 26f
        textAlign = Paint.Align.CENTER
    }
    canvas.drawText("已用", size / 2f, size / 2f + 44f, labelPaint)
    return bitmap
}

/**
 * Mini area chart rendered to a bitmap from daily cumulative points:
 * gradient fill, exact data dots, first/last values — mirrors the app's
 * chart card so the widget reads as the same product.
 */
private fun chartBitmap(points: List<CdtApi.HistoryPoint>, dark: Boolean): Bitmap {
    val width = 672
    val height = 176
    val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
    val canvas = Canvas(bitmap)
    val values = points.map { it.traffic }
    val max = (values.maxOrNull() ?: 0.0) * 1.12 + 1.0
    val padTop = 30f
    val padBottom = 26f
    val usableH = height - padTop - padBottom
    fun x(i: Int): Float = if (values.size <= 1) width / 2f else i * width.toFloat() / (values.size - 1)
    fun y(v: Double): Float = padTop + ((1.0 - v / max) * usableH).toFloat()

    val lineColor = if (dark) 0xFF3FA67F.toInt() else 0xFF1E5B4A.toInt()
    val fillShader = android.graphics.LinearGradient(
        0f, padTop, 0f, height - padBottom,
        if (dark) 0x553FA67F else 0x551E8A4C,
        0x00000000,
        android.graphics.Shader.TileMode.CLAMP,
    )
    val fillPath = android.graphics.Path().apply {
        moveTo(x(0), y(values.first()))
        for (i in 1 until values.size) lineTo(x(i), y(values[i]))
        lineTo(x(values.lastIndex), height - padBottom)
        lineTo(x(0), height - padBottom)
        close()
    }
    canvas.drawPath(fillPath, Paint(Paint.ANTI_ALIAS_FLAG).apply { shader = fillShader })
    val linePath = android.graphics.Path().apply {
        moveTo(x(0), y(values.first()))
        for (i in 1 until values.size) lineTo(x(i), y(values[i]))
    }
    canvas.drawPath(linePath, Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 3.5f
        strokeJoin = Paint.Join.ROUND
        strokeCap = Paint.Cap.ROUND
        color = lineColor
    })
    val dotFill = if (dark) 0xFF10150F.toInt() else 0xFFFFFFFF.toInt()
    values.forEachIndexed { i, v ->
        if (i == values.lastIndex) {
            canvas.drawCircle(x(i), y(v), 7f, Paint(Paint.ANTI_ALIAS_FLAG).apply { color = lineColor })
        } else {
            canvas.drawCircle(x(i), y(v), 6f, Paint(Paint.ANTI_ALIAS_FLAG).apply { color = lineColor })
            canvas.drawCircle(x(i), y(v), 3f, Paint(Paint.ANTI_ALIAS_FLAG).apply { color = dotFill })
        }
    }
    val ink = if (dark) 0xFFEFF4F0.toInt() else 0xFF15211B.toInt()
    val muted = if (dark) 0xFF9DB0A5.toInt() else 0xFF6E7F76.toInt()
    val valuePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = lineColor
        textSize = 24f
        typeface = Typeface.create(Typeface.DEFAULT, Typeface.BOLD)
    }
    valuePaint.textAlign = Paint.Align.LEFT
    canvas.drawText(String.format(Locale.getDefault(), "%.1f", values.first()), x(0), y(values.first()) - 12f, valuePaint)
    valuePaint.textAlign = Paint.Align.RIGHT
    canvas.drawText(
        String.format(Locale.getDefault(), "%.1f GB", values.last()),
        x(values.lastIndex), y(values.last()) - 12f, valuePaint,
    )
    val datePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { color = muted; textSize = 21f }
    fun shortDate(raw: String): String {
        val date = raw.substringBefore('T').substringBefore(' ')
        return if (date.length >= 10) date.substring(5, 10) else date
    }
    datePaint.textAlign = Paint.Align.LEFT
    canvas.drawText(shortDate(points.first().at), x(0), height - 4f, datePaint)
    datePaint.textAlign = Paint.Align.RIGHT
    canvas.drawText(shortDate(points.last().at), x(points.lastIndex), height - 4f, datePaint)
    return bitmap
}

@Composable
private fun ChartImage(points: List<CdtApi.HistoryPoint>) {
    val context = LocalContext.current
    val dark = (context.resources.configuration.uiMode and
        Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
    val daily = points.takeLast(7)
    val bitmap = remember(daily.map { it.traffic }, dark) { chartBitmap(daily, dark) }
    Image(
        provider = ImageProvider(bitmap),
        contentDescription = "近 7 日已用流量曲线",
        modifier = GlanceModifier.fillMaxWidth().height(58.dp),
        contentScale = ContentScale.FillBounds,
    )
}

@Composable
private fun RingImage(pct: Double, sizeDp: androidx.compose.ui.unit.Dp) {
    val context = LocalContext.current
    val dark = (context.resources.configuration.uiMode and
        Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
    val bitmap = remember(pct, dark) { ringBitmap(pct, dark) }
    Image(
        provider = ImageProvider(bitmap),
        contentDescription = "已用百分比",
        modifier = GlanceModifier.size(sizeDp),
    )
}


/** Mint server tile used as the identity mark in every widget layout. */
@Composable
private fun ServerTile(sizeDp: androidx.compose.ui.unit.Dp) {
    Box(
        modifier = GlanceModifier
            .size(sizeDp)
            .background(ImageProvider(R.drawable.widget_server_tile)),
        contentAlignment = Alignment.Center,
    ) {
        Image(
            provider = ImageProvider(R.drawable.ic_server),
            contentDescription = null,
            modifier = GlanceModifier.size(sizeDp * 0.66f),
        )
    }
}

/** Circular white refresh button, as in the reference layouts. */
@Composable
private fun RefreshCircle() {
    Box(
        modifier = GlanceModifier
            .size(34.dp)
            .background(ImageProvider(R.drawable.widget_circle_btn))
            .clickable(actionRunCallback<RefreshAction>()),
        contentAlignment = Alignment.Center,
    ) {
        Image(
            provider = ImageProvider(R.drawable.ic_refresh),
            contentDescription = "刷新",
            modifier = GlanceModifier.size(17.dp),
        )
    }
}

/** Label-over-value billing column used in square / rich footers. */
@Composable
private fun BillingCol(label: String, value: String, modifier: GlanceModifier = GlanceModifier) {
    Column(modifier = modifier) {
        Text(
            text = label,
            style = TextStyle(color = MutedProvider, fontSize = 10.5.sp),
        )
        Spacer(modifier = GlanceModifier.height(2.dp))
        Text(
            text = value,
            style = TextStyle(color = InkProvider, fontSize = 14.5.sp, fontWeight = FontWeight.Bold),
        )
    }
}

@Composable
private fun VDivider(heightDp: androidx.compose.ui.unit.Dp) {
    Box(
        modifier = GlanceModifier
            .width(1.dp)
            .height(heightDp)
            .background(TrackProvider),
    ) {}
}

@Composable
private fun HDivider() {
    Box(
        modifier = GlanceModifier
            .fillMaxWidth()
            .height(1.dp)
            .background(TrackProvider),
    ) {}
}

@Composable
private fun StatusPill(account: CdtApi.Account) {
    Box(
        modifier = GlanceModifier
            .background(statusColorProvider(account.status))
            .cornerRadius(10.dp)
            .padding(horizontal = 8.dp, vertical = 2.dp),
    ) {
        Text(
            text = account.displayStatus(),
            style = TextStyle(color = PaperProvider, fontSize = 11.sp),
        )
    }
}

@Composable
private fun RefreshButton() {
    Box(
        modifier = GlanceModifier
            .size(30.dp)
            .background(ImageProvider(R.drawable.widget_icon_circle))
            .clickable(actionRunCallback<RefreshAction>()),
        contentAlignment = Alignment.Center,
    ) {
        Image(
            provider = ImageProvider(R.drawable.ic_refresh),
            contentDescription = "刷新",
            modifier = GlanceModifier.size(16.dp),
        )
    }
}

/**
 * Thick usage bar — the widget's main visual. Built from two boxes with an
 * explicitly computed fill width (Glance has no proportional weights), so
 * the height and rounded ends are exact at every widget size.
 */
@Composable
private fun ThickBar(pct: Double, widthDp: androidx.compose.ui.unit.Dp, height: androidx.compose.ui.unit.Dp = 10.dp) {
    val p = pct.coerceIn(0.0, 100.0)
    val color = when {
        p >= 90.0 -> RedProvider
        p >= 75.0 -> OrangeProvider
        else -> GreenProvider
    }
    Box(
        modifier = GlanceModifier
            .width(widthDp)
            .height(height)
            .background(TrackProvider)
            .cornerRadius(height / 2),
    ) {
        if (p > 0.5) {
            val fillWidth = (widthDp * (p / 100.0).toFloat()).coerceAtLeast(height)
            Box(
                modifier = GlanceModifier
                    .width(fillWidth)
                    .height(height)
                    .background(color)
                    .cornerRadius(height / 2),
            ) {}
        }
    }
}

@Composable
private fun FlatContent(summary: CdtApi.Summary, selectedId: Long, detail: CdtApi.StatusAccount?) {
    val account = summary.find(selectedId) ?: summary.accounts.firstOrNull()
    if (account == null) {
        LoadingContent()
        return
    }
    val size = LocalSize.current
    Column(modifier = GlanceModifier.fillMaxSize().padding(horizontal = 14.dp, vertical = 10.dp)) {
        Row(
            modifier = GlanceModifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ServerTile(40.dp)
            Spacer(modifier = GlanceModifier.width(10.dp))
            Column(modifier = GlanceModifier.defaultWeight()) {
                Text(
                    text = account.displayName(),
                    style = TextStyle(color = InkProvider, fontSize = 13.sp, fontWeight = FontWeight.Bold),
                    maxLines = 1,
                )
                Spacer(modifier = GlanceModifier.height(4.dp))
                StatusPill(account)
            }
            Column {
                Text(
                    text = String.format(Locale.getDefault(), "%.2f", account.used),
                    style = TextStyle(color = InkProvider, fontSize = 20.sp, fontWeight = FontWeight.Bold),
                    maxLines = 1,
                )
                Text(
                    text = String.format(Locale.getDefault(), "%.1f%% · 共 %.0f GB", account.percentage, account.total),
                    style = TextStyle(color = MutedProvider, fontSize = 10.sp),
                    maxLines = 1,
                )
            }
            Spacer(modifier = GlanceModifier.width(10.dp))
            RefreshCircle()
        }
        Spacer(modifier = GlanceModifier.height(7.dp))
        ThickBar(account.percentage, size.width - 28.dp, height = 6.dp)
    }
}

@Composable
private fun HeroRow(account: CdtApi.Account, detail: CdtApi.StatusAccount?) {
    Row(
        modifier = GlanceModifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        RingImage(account.percentage, 84.dp)
        Spacer(modifier = GlanceModifier.width(14.dp))
        Column(modifier = GlanceModifier.defaultWeight()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = GlanceModifier.size(8.dp).background(GreenProvider).cornerRadius(4.dp),
                ) {}
                Spacer(modifier = GlanceModifier.width(7.dp))
                Text(
                    text = String.format(Locale.getDefault(), "已用 %.2f GB", account.used),
                    style = TextStyle(color = InkProvider, fontSize = 15.sp, fontWeight = FontWeight.Bold),
                )
            }
            Spacer(modifier = GlanceModifier.height(5.dp))
            Row(verticalAlignment = Alignment.CenterVertically) {
                Box(
                    modifier = GlanceModifier.size(8.dp).background(TrackProvider).cornerRadius(4.dp),
                ) {}
                Spacer(modifier = GlanceModifier.width(7.dp))
                Text(
                    text = String.format(
                        Locale.getDefault(), "剩余 %.2f GB",
                        (account.total - account.used).coerceAtLeast(0.0),
                    ),
                    style = TextStyle(color = InkProvider, fontSize = 13.sp),
                )
            }
            Spacer(modifier = GlanceModifier.height(9.dp))
            Box(
                modifier = GlanceModifier.fillMaxWidth().height(1.dp).background(TrackProvider),
            ) {}
            Spacer(modifier = GlanceModifier.height(9.dp))
            Text(
                text = String.format(Locale.getDefault(), "总配额 %.0f GB", account.total),
                style = TextStyle(color = MutedProvider, fontSize = 11.sp),
            )
        }
    }
}

@Composable
private fun RichContent(
    summary: CdtApi.Summary,
    selectedId: Long,
    error: String,
    loading: Boolean,
    detail: CdtApi.StatusAccount?,
) {
    val account = summary.find(selectedId) ?: summary.accounts.firstOrNull()
    if (account == null) {
        LoadingContent()
        return
    }
    val size = LocalSize.current
    Column(modifier = GlanceModifier.fillMaxSize().padding(16.dp)) {
        Row(
            modifier = GlanceModifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ServerTile(42.dp)
            Spacer(modifier = GlanceModifier.width(11.dp))
            Column(modifier = GlanceModifier.defaultWeight()) {
                Text(
                    text = account.displayName(),
                    style = TextStyle(color = InkProvider, fontSize = 15.sp, fontWeight = FontWeight.Bold),
                    maxLines = 1,
                )
                Spacer(modifier = GlanceModifier.height(4.dp))
                StatusPill(account)
            }
            RefreshCircle()
        }
        Spacer(modifier = GlanceModifier.height(9.dp))
        Text(
            text = buildList {
                if (detail != null && detail.displayRegion().isNotEmpty()) add(detail.displayRegion())
                if (detail != null && detail.publicIp.isNotEmpty()) add("IP ${detail.publicIp}")
            }.joinToString(" · ").ifEmpty {
                if (loading) "正在更新…" else "更新 ${formatTimestamp(account.updatedAt)}"
            },
            style = TextStyle(color = MutedProvider, fontSize = 11.5.sp),
            maxLines = 1,
        )
        Spacer(modifier = GlanceModifier.height(8.dp))
        Row(verticalAlignment = Alignment.Bottom) {
            Text(
                text = String.format(Locale.getDefault(), "%.2f", account.used),
                style = TextStyle(color = InkProvider, fontSize = 27.sp, fontWeight = FontWeight.Bold),
                maxLines = 1,
            )
            Text(
                text = String.format(Locale.getDefault(), " / %.0f GB", account.total),
                style = TextStyle(color = MutedProvider, fontSize = 12.sp),
                modifier = GlanceModifier.padding(bottom = 4.dp),
                maxLines = 1,
            )
        }
        Spacer(modifier = GlanceModifier.height(6.dp))
        ThickBar(account.percentage, size.width - 32.dp, height = 8.dp)
        Spacer(modifier = GlanceModifier.height(7.dp))
        Row(modifier = GlanceModifier.fillMaxWidth()) {
            Text(
                text = String.format(Locale.getDefault(), "%.2f%% 已使用", account.percentage),
                style = TextStyle(color = MutedProvider, fontSize = 11.5.sp),
                modifier = GlanceModifier.defaultWeight(),
                maxLines = 1,
            )
            if (detail != null && detail.threshold > 0) {
                Text(
                    text = "阈值 ${detail.threshold}%",
                    style = TextStyle(color = MutedProvider, fontSize = 11.5.sp),
                    maxLines = 1,
                )
            } else if (error.isNotEmpty()) {
                Text(
                    text = "刷新失败，显示上次数据",
                    style = TextStyle(color = RedProvider, fontSize = 11.5.sp),
                    maxLines = 1,
                )
            }
        }
        Spacer(modifier = GlanceModifier.defaultWeight())
        HDivider()
        Spacer(modifier = GlanceModifier.height(9.dp))
        if (detail != null && (detail.monthlyCost != null || detail.balance != null)) {
            Row(modifier = GlanceModifier.fillMaxWidth()) {
                BillingCol(
                    "本月费用",
                    detail.monthlyCost?.let { detail.displayMoney(it) } ?: "—",
                    modifier = GlanceModifier.defaultWeight(),
                )
                VDivider(26.dp)
                BillingCol(
                    "账户余额",
                    detail.balance?.let { detail.displayMoney(it) } ?: "—",
                    modifier = GlanceModifier.defaultWeight(),
                )
            }
        } else {
            Text(
                text = String.format(
                    Locale.getDefault(), "剩余 %.2f GB · 更新 %s",
                    (account.total - account.used).coerceAtLeast(0.0),
                    formatTimestamp(account.updatedAt),
                ),
                style = TextStyle(color = MutedProvider, fontSize = 11.sp),
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun LargeContent(
    summary: CdtApi.Summary,
    selectedId: Long,
    error: String,
) {
    Column(modifier = GlanceModifier.fillMaxSize().padding(16.dp)) {
        Row(
            modifier = GlanceModifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ServerTile(34.dp)
            Spacer(modifier = GlanceModifier.width(10.dp))
            Text(
                text = "我的实例 (${summary.accounts.size})",
                style = TextStyle(color = InkProvider, fontSize = 15.sp, fontWeight = FontWeight.Bold),
                modifier = GlanceModifier.defaultWeight(),
            )
            RefreshCircle()
        }
        Spacer(modifier = GlanceModifier.height(12.dp))
        summary.accounts.take(4).forEach { account ->
            InstanceRowCard(account, account.id == (summary.find(selectedId)?.id ?: summary.accounts.firstOrNull()?.id))
            Spacer(modifier = GlanceModifier.height(9.dp))
        }
        Spacer(modifier = GlanceModifier.defaultWeight())
        Row(modifier = GlanceModifier.fillMaxWidth()) {
            Spacer(modifier = GlanceModifier.defaultWeight())
            Text(
                text = if (error.isNotEmpty()) {
                    "刷新失败，显示上次数据"
                } else {
                    "更新 ${formatTimestamp(summary.updatedAt)}"
                },
                style = TextStyle(
                    color = if (error.isNotEmpty()) RedProvider else MutedProvider,
                    fontSize = 11.sp,
                ),
            )
        }
    }
}

@Composable
private fun InstanceRowCard(account: CdtApi.Account, selected: Boolean) {
    Box(
        modifier = GlanceModifier
            .fillMaxWidth()
            .background(ImageProvider(R.drawable.widget_row_bg))
            .cornerRadius(14.dp)
            .clickable(actionRunCallback<SwitchAccountAction>(actionParametersOf(SwitchAccountAction.KEY_ACCOUNT_ID to account.id)))
            .padding(horizontal = 11.dp, vertical = 10.dp),
    ) {
        Row(
            modifier = GlanceModifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            ServerTile(34.dp)
            Spacer(modifier = GlanceModifier.width(9.dp))
            Column(modifier = GlanceModifier.defaultWeight()) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = account.displayName(),
                        style = TextStyle(color = InkProvider, fontSize = 13.sp, fontWeight = FontWeight.Bold),
                        maxLines = 1,
                        modifier = GlanceModifier.defaultWeight(),
                    )
                    Spacer(modifier = GlanceModifier.width(6.dp))
                    StatusPill(account)
                }
                Spacer(modifier = GlanceModifier.height(5.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = String.format(Locale.getDefault(), "%.2f / %.0f GB", account.used, account.total),
                        style = TextStyle(color = InkProvider, fontSize = 12.sp, fontWeight = FontWeight.Bold),
                    )
                    Spacer(modifier = GlanceModifier.width(9.dp))
                    ThickBar(account.percentage, 72.dp, height = 5.dp)
                }
            }
            Spacer(modifier = GlanceModifier.width(6.dp))
            Text(
                text = "›",
                style = TextStyle(color = MutedProvider, fontSize = 17.sp, fontWeight = FontWeight.Bold),
            )
        }
    }
}

/** "2026-11-11T15:17:00Z"-style timestamps -> "11-11 15:17" local. */
internal fun formatTimestamp(raw: String): String {
    if (raw.isBlank()) return "—"
    return try {
        val instant = java.time.Instant.parse(raw)
        java.time.format.DateTimeFormatter.ofPattern("MM-dd HH:mm")
            .withZone(java.time.ZoneId.systemDefault())
            .format(instant)
    } catch (ignored: Exception) {
        val cleaned = raw.replace('T', ' ').substringBefore('.').removeSuffix("Z")
        if (cleaned.length >= 16) cleaned.substring(5, 16) else cleaned
    }
}
