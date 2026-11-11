package com.wang4386.cdtmonitor.widget

import android.content.Context
import android.content.SharedPreferences

/**
 * Persisted connection settings, per-widget instance selection and the last
 * successfully fetched summary (used as a stale-data fallback so a failed
 * refresh never blanks the widget).
 *
 * The file name and keys are unchanged from the Java version: upgrades keep
 * the saved site URL, encrypted API key and widget selections.
 */
object AppPreferences {
    private const val FILE = "cdt_monitor_widget"
    private const val SITE_URL = "site_url"
    private const val API_KEY = "api_key"
    private const val CONTROL_KEY = "control_key"
    private const val CONTROL_KEY_ALIAS = "cdt-monitor-widget-control-key"
    private const val WIDGET_ACCOUNT_PREFIX = "widget_account_"
    private const val LAST_SUMMARY_JSON = "last_summary_json"
    private const val LAST_SUMMARY_AT = "last_summary_at"
    private const val LAST_STATUS_JSON = "last_status_json"

    private fun prefs(context: Context): SharedPreferences =
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    fun siteUrl(context: Context): String = prefs(context).getString(SITE_URL, "") ?: ""

    fun apiKey(context: Context): String = SecretStore.decrypt(prefs(context).getString(API_KEY, ""))

    fun saveConnection(context: Context, siteUrl: String, apiKey: String): Boolean {
        val encrypted = SecretStore.encrypt(apiKey)
        if (encrypted.isEmpty()) return false
        return prefs(context).edit()
            .putString(SITE_URL, siteUrl)
            .putString(API_KEY, encrypted)
            .commit()
    }

    fun hasConnection(context: Context): Boolean =
        siteUrl(context).isNotEmpty() && apiKey(context).isNotEmpty()

    /** Control key (instance:control), stored under its own Keystore alias. */
    fun controlKey(context: Context): String =
        SecretStore.decrypt(prefs(context).getString(CONTROL_KEY, ""), CONTROL_KEY_ALIAS)

    fun saveControlKey(context: Context, key: String): Boolean {
        val encrypted = SecretStore.encrypt(key, CONTROL_KEY_ALIAS)
        if (encrypted.isEmpty()) return false
        return prefs(context).edit().putString(CONTROL_KEY, encrypted).commit()
    }

    fun hasControlKey(context: Context): Boolean = controlKey(context).isNotEmpty()

    fun clearControlKey(context: Context) {
        prefs(context).edit().remove(CONTROL_KEY).apply()
    }

    fun saveWidgetAccount(context: Context, widgetId: Int, accountId: Long) {
        prefs(context).edit().putLong(WIDGET_ACCOUNT_PREFIX + widgetId, accountId).apply()
    }

    fun widgetAccount(context: Context, widgetId: Int): Long =
        prefs(context).getLong(WIDGET_ACCOUNT_PREFIX + widgetId, -1L)

    fun removeWidget(context: Context, widgetId: Int) {
        prefs(context).edit().remove(WIDGET_ACCOUNT_PREFIX + widgetId).apply()
    }

    /** Removes saved selections whose widget id is no longer placed. */
    fun pruneStaleWidgets(context: Context, activeIds: Set<Int>) {
        val editor = prefs(context).edit()
        var changed = false
        for (key in prefs(context).all.keys) {
            if (key.startsWith(WIDGET_ACCOUNT_PREFIX)) {
                val id = key.removePrefix(WIDGET_ACCOUNT_PREFIX).toIntOrNull()
                if (id != null && id !in activeIds) {
                    editor.remove(key)
                    changed = true
                }
            }
        }
        if (changed) editor.apply()
    }

    fun saveLastSummary(context: Context, summary: CdtApi.Summary) {
        prefs(context).edit()
            .putString(LAST_SUMMARY_JSON, summary.toJson().toString())
            .putLong(LAST_SUMMARY_AT, System.currentTimeMillis())
            .apply()
    }

    fun lastSummary(context: Context): CdtApi.Summary? {
        val json = prefs(context).getString(LAST_SUMMARY_JSON, null) ?: return null
        return try {
            CdtApi.Summary.fromJson(org.json.JSONObject(json))
        } catch (ignored: Exception) {
            null
        }
    }

    /** Epoch millis of the last successful fetch, or 0 when never fetched. */
    fun lastSummaryAt(context: Context): Long = prefs(context).getLong(LAST_SUMMARY_AT, 0L)

    /** Raw /api/v1/status payload cache so the dashboard renders instantly. */
    fun saveLastStatus(context: Context, rawJson: String) {
        prefs(context).edit().putString(LAST_STATUS_JSON, rawJson).apply()
    }

    fun lastStatus(context: Context): CdtApi.StatusResponse? {
        val json = prefs(context).getString(LAST_STATUS_JSON, null) ?: return null
        return try {
            CdtApi.parseStatus(org.json.JSONObject(json))
        } catch (ignored: Exception) {
            null
        }
    }
}
