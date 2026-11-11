package com.wang4386.cdtmonitor.widget

import androidx.datastore.preferences.core.Preferences
import androidx.datastore.preferences.core.booleanPreferencesKey
import androidx.datastore.preferences.core.longPreferencesKey
import androidx.datastore.preferences.core.stringPreferencesKey

/**
 * Per-widget Glance state keys (Preferences-based state definition).
 *
 * The worker is the only writer of fetched data; composables only read.
 * Keeping the last good summary in state is what lets the widget show stale
 * data with an error note instead of blanking out on a failed refresh.
 */
object WidgetState {
    val ACCOUNT_ID = longPreferencesKey("account_id")
    val SUMMARY_JSON = stringPreferencesKey("summary_json")
    val ERROR = stringPreferencesKey("error")
    val LOADING = booleanPreferencesKey("loading")
    val FETCHED_AT = longPreferencesKey("fetched_at")
    val HISTORY_JSON = stringPreferencesKey("history_json")

    fun accountId(prefs: Preferences, fallback: Long): Long =
        prefs[ACCOUNT_ID]?.takeIf { it > 0 } ?: fallback

    /** Daily cumulative points for the selected account, oldest first. */
    fun history(prefs: Preferences): List<CdtApi.HistoryPoint> {
        val json = prefs[HISTORY_JSON] ?: return emptyList()
        return try {
            val array = org.json.JSONArray(json)
            val points = ArrayList<CdtApi.HistoryPoint>(array.length())
            for (i in 0 until array.length()) {
                val raw = array.getJSONObject(i)
                points += CdtApi.HistoryPoint(
                    at = raw.optString("at", ""),
                    traffic = raw.optDouble("traffic", 0.0),
                )
            }
            points
        } catch (ignored: Exception) {
            emptyList()
        }
    }

    fun summary(prefs: Preferences): CdtApi.Summary? {
        val json = prefs[SUMMARY_JSON] ?: return null
        return try {
            CdtApi.Summary.fromJson(org.json.JSONObject(json))
        } catch (ignored: Exception) {
            null
        }
    }
}
