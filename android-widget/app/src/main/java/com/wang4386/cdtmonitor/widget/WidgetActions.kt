package com.wang4386.cdtmonitor.widget

import android.content.Context
import androidx.glance.GlanceId
import androidx.glance.action.ActionParameters
import androidx.glance.appwidget.GlanceAppWidgetManager
import androidx.glance.appwidget.action.ActionCallback
import androidx.glance.appwidget.state.updateAppWidgetState

/** Refresh button on the widget: mark loading, then let the worker fetch. */
class RefreshAction : ActionCallback {
    override suspend fun onAction(
        context: Context,
        glanceId: GlanceId,
        parameters: ActionParameters,
    ) {
        val appWidgetId = GlanceAppWidgetManager(context).getAppWidgetId(glanceId)
        updateAppWidgetState(context, glanceId) { prefs ->
            prefs[WidgetState.LOADING] = true
            prefs[WidgetState.ERROR] = ""
        }
        CdtWidget().update(context, glanceId)
        RefreshWorker.enqueueNow(context, appWidgetId)
    }
}

/** Tap an instance row in the large widget to pin it as the selected one. */
class SwitchAccountAction : ActionCallback {
    override suspend fun onAction(
        context: Context,
        glanceId: GlanceId,
        parameters: ActionParameters,
    ) {
        val accountId = parameters[KEY_ACCOUNT_ID] ?: return
        val appWidgetId = GlanceAppWidgetManager(context).getAppWidgetId(glanceId)
        if (appWidgetId > 0) {
            AppPreferences.saveWidgetAccount(context, appWidgetId, accountId)
        }
        updateAppWidgetState(context, glanceId) { prefs ->
            prefs[WidgetState.ACCOUNT_ID] = accountId
        }
        CdtWidget().update(context, glanceId)
    }

    companion object {
        val KEY_ACCOUNT_ID = ActionParameters.Key<Long>("account_id")
    }
}
