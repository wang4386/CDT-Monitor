package com.wang4386.cdtmonitor.widget

import android.content.Context
import androidx.glance.GlanceId
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.glance.appwidget.state.updateAppWidgetState
import androidx.glance.appwidget.updateAll
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.NetworkType
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import java.util.concurrent.TimeUnit

/**
 * Single fetch path for every widget refresh: periodic (30 min), manual
 * (refresh button), after configuration and after saving settings.
 *
 * Following the official glance-appwidget-updates guidance, background work
 * runs in WorkManager (not in the broadcast receiver), state is pushed with
 * updateAppWidgetState, and an explicit update is triggered afterwards.
 * A failed fetch keeps the previous summary in state and only records the
 * error, so the widget degrades to stale data instead of an error screen.
 */
class RefreshWorker(
    appContext: Context,
    params: WorkerParameters,
) : CoroutineWorker(appContext, params) {

    override suspend fun doWork(): Result {
        val context = applicationContext
        val manager = GlanceAppWidgetManager(context)
        val allIds = runCatching { manager.getGlanceIds(CdtWidget::class.java) }
            .getOrDefault(emptyList())
        if (allIds.isEmpty()) return Result.success()

        val targetAppWidgetId = inputData.getInt(KEY_APP_WIDGET_ID, -1)
        val targets = if (targetAppWidgetId > 0) {
            allIds.filter { manager.getAppWidgetId(it) == targetAppWidgetId }
        } else {
            allIds
        }
        if (targets.isEmpty()) return Result.success()

        if (!AppPreferences.hasConnection(context)) {
            pushState(context, targets) { prefs ->
                prefs[WidgetState.ERROR] = "尚未配置连接"
                prefs[WidgetState.LOADING] = false
            }
            return Result.success()
        }

        val fetched = runCatching {
            CdtApi.fetchSummary(AppPreferences.siteUrl(context), AppPreferences.apiKey(context))
        }
        return if (fetched.isSuccess) {
            val summary = fetched.getOrThrow()
            AppPreferences.saveLastSummary(context, summary)
            // Best-effort: cache the full status too, so larger widget sizes
            // can show the selected instance's IP / billing detail lines.
            runCatching {
                val (_, rawStatus) = CdtApi.fetchStatusWithRaw(
                    AppPreferences.siteUrl(context),
                    AppPreferences.apiKey(context),
                )
                AppPreferences.saveLastStatus(context, rawStatus)
            }
            // Best-effort: daily history per selected account, for the
            // chart block in larger widget sizes.
            val histories = HashMap<Long, String>()
            val wantedAccounts = targets.map { glanceId ->
                AppPreferences.widgetAccount(context, manager.getAppWidgetId(glanceId))
            }.toMutableSet()
            summary.accounts.firstOrNull()?.let { wantedAccounts += it.id }
            for (accountId in wantedAccounts) {
                if (accountId <= 0 || summary.accounts.none { it.id == accountId }) continue
                runCatching {
                    val points = CdtApi.fetchDailyHistory(
                        AppPreferences.siteUrl(context),
                        AppPreferences.apiKey(context),
                        accountId,
                    )
                    val array = org.json.JSONArray()
                    for (point in points) {
                        array.put(org.json.JSONObject().put("at", point.at).put("traffic", point.traffic))
                    }
                    histories[accountId] = array.toString()
                }
            }
            val json = summary.toJson().toString()
            val now = System.currentTimeMillis()
            for (glanceId in targets) {
                val selected = AppPreferences.widgetAccount(
                    context,
                    manager.getAppWidgetId(glanceId),
                )
                val accountId = if (summary.accounts.any { it.id == selected }) {
                    selected
                } else {
                    summary.accounts.firstOrNull()?.id ?: 0L
                }
                updateAppWidgetState(context, glanceId) { prefs ->
                    prefs[WidgetState.SUMMARY_JSON] = json
                    prefs[WidgetState.ERROR] = ""
                    prefs[WidgetState.LOADING] = false
                    prefs[WidgetState.FETCHED_AT] = now
                    histories[accountId]?.let { prefs[WidgetState.HISTORY_JSON] = it }
                }
            }
            CdtWidget().updateAll(context)
            Result.success()
        } else {
            val message = fetched.exceptionOrNull()?.message ?: "刷新失败"
            pushState(context, targets) { prefs ->
                // SUMMARY_JSON is intentionally untouched: stale data stays visible.
                prefs[WidgetState.ERROR] = message
                prefs[WidgetState.LOADING] = false
            }
            Result.success()
        }
    }

    private suspend fun pushState(
        context: Context,
        glanceIds: List<GlanceId>,
        mutate: (androidx.datastore.preferences.core.MutablePreferences) -> Unit,
    ) {
        for (glanceId in glanceIds) {
            updateAppWidgetState(context, glanceId) { prefs -> mutate(prefs) }
        }
        CdtWidget().updateAll(context)
    }

    companion object {
        private const val KEY_APP_WIDGET_ID = "app_widget_id"
        private const val PERIODIC_NAME = "cdt-widget-periodic"
        private const val ONETIME_NAME = "cdt-widget-refresh"

        fun enqueuePeriodic(context: Context) {
            val request = PeriodicWorkRequestBuilder<RefreshWorker>(30, TimeUnit.MINUTES)
                .setConstraints(
                    Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build(),
                )
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(
                PERIODIC_NAME,
                ExistingPeriodicWorkPolicy.KEEP,
                request,
            )
        }

        /** Refresh all widgets, or only [appWidgetId] when given. */
        fun enqueueNow(context: Context, appWidgetId: Int? = null) {
            val request = OneTimeWorkRequestBuilder<RefreshWorker>()
                .setConstraints(
                    Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build(),
                )
                .setInputData(workDataOf(KEY_APP_WIDGET_ID to (appWidgetId ?: -1)))
                .build()
            WorkManager.getInstance(context).enqueueUniqueWork(
                if (appWidgetId != null) "$ONETIME_NAME-$appWidgetId" else ONETIME_NAME,
                ExistingWorkPolicy.REPLACE,
                request,
            )
        }
    }
}
